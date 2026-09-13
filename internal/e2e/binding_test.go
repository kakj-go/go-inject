package e2e

import "testing"

func TestSemanticTypeIdentity(t *testing.T) {
	cases := []struct {
		name, lib, rule, main string
		extra                 map[string]string
	}{
		{"imported-alias", "package lib\nimport \"example.test/app/model\"\nfunc F(v model.Text)string{return v}\n", "func F(v string)(r string){defer func(){r+=\"!\"}();return \"\"}", "lib.F(\"x\")", map[string]string{"model/model.go": "package model\ntype Text = string\n"}},
		{"generic-alias", "package lib\ntype Items[T any] = []T\nfunc F(v Items[int])int{return len(v)}\n", "func F(v []int)(r int){defer func(){r++}();return 0}", "lib.F([]int{1})", nil},
		{"array-constant", "package lib\nconst Size=2\nfunc F(v [Size]int)int{return len(v)}\n", "func F(v [2]int)(r int){defer func(){r++}();return 0}", "lib.F([2]int{})", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"main.go": "package main\nimport \"example.test/app/lib\"\nfunc main(){println(" + tc.main + ")}\n", "inject.go": registration, "lib/lib.go": tc.lib, "hooks/rule.go": "//inject:example.test/app/lib/lib.go\npackage hooks\n" + tc.rule + "\n"}
			for k, v := range tc.extra {
				files[k] = v
			}
			f := newFixture(t, files)
			f.cli("build", "-o", "app"+exeSuffix(), ".")
			want := "x!"
			if tc.name == "generic-alias" {
				want = "2"
			}
			if tc.name == "array-constant" {
				want = "3"
			}
			f.run("app", want)
		})
	}
}

func TestSemanticGenericReceiverAndProjection(t *testing.T) {
	f := newFixture(t, map[string]string{
		"main.go":       "package main\nimport \"example.test/app/lib\"\nfunc main(){v:=lib.Box[int]{Value:3};println(v.Get())}\n",
		"inject.go":     registration,
		"lib/lib.go":    "package lib\ntype Box[T ~int]struct{Value T}\nfunc(b *Box[T])Get()T{return b.Value}\n",
		"hooks/rule.go": "//inject:example.test/app/lib/lib.go\npackage hooks\ntype Box[U ~int]struct{Value U}\nfunc(b *Box[U])Get()(r U){defer func(){r++}();return 0}\n",
	})
	f.cli("build", "-o", "app"+exeSuffix(), ".")
	f.run("app", "4")
}

func TestSemanticBuiltinShadowDoesNotHideMismatch(t *testing.T) {
	f := newFixture(t, map[string]string{"main.go": "package main\nimport \"example.test/app/lib\"\nfunc main(){lib.F(\"x\")}\n", "inject.go": registration,
		"lib/lib.go":    "package lib\ntype int string\nfunc F(v int)string{return string(v)}\n",
		"hooks/rule.go": "//inject:example.test/app/lib/lib.go\npackage hooks\nfunc F(v int)string{return \"\"}\n"})
	f.fails("signature mismatch", "build", ".")
}

func TestDependentReceiverConstraintsUseTemplateNames(t *testing.T) {
	f := newFixture(t, map[string]string{
		"main.go":       "package main\nimport \"example.test/app/lib\"\nfunc main(){v:=lib.Box[int,[]int]{Value:[]int{1,2}};println(v.Size())}\n",
		"inject.go":     registration,
		"lib/lib.go":    "package lib\ntype Box[A any,B ~[]A]struct{Value B}\nfunc(v *Box[A,B])Size()int{return len(v.Value)}\n",
		"hooks/rule.go": "//inject:example.test/app/lib/lib.go\npackage hooks\ntype Box[X any,Y ~[]X]struct{Value Y}\nfunc(v *Box[X,Y])Size()(n int){defer func(){n++}();return 0}\n",
	})
	f.cli("build", "-o", "app"+exeSuffix(), ".")
	f.run("app", "3")
}

func TestMainRuleBindsRealHelperFile(t *testing.T) {
	f := newFixture(t, map[string]string{"main.go": "package main\nfunc main(){println(helper())}\n", "helpers.go": "package main\nfunc helper()int{return 1}\n", "inject.go": registration,
		"hooks/rule.go": "//inject:main\npackage hooks\nfunc helper()(r int){defer func(){r++}();return 0}\n"})
	f.cli("build", "-o", "app"+exeSuffix(), ".")
	f.run("app", "2")
}

func TestInternalTestFileCanBeTargeted(t *testing.T) {
	f := newFixture(t, map[string]string{"lib/lib.go": "package lib\nfunc Value()int{return 1}\n",
		"lib/helpers_test.go": "package lib\nimport \"testing\"\nfunc helper()int{return 1}\nfunc TestHelper(t *testing.T){if helper()!=2{t.Fatal(\"hook missing\")}}\n",
		"lib/inject_test.go":  "//go:build goinject || generate\n\npackage lib\nimport _ \"example.test/app/hooks\"\n",
		"hooks/rule.go":       "//inject:example.test/app/lib/helpers_test.go\npackage hooks\nfunc helper()(r int){defer func(){r++}();return 0}\n"})
	f.cli("test", "./lib")
}
