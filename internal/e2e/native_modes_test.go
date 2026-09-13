package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeCompatibleEntriesShareCompilation(t *testing.T) {
	files := map[string]string{"lib/lib.go": "package lib\nfunc Value()int{return 1}\n", "hooks/rule.go": "//inject:example.test/app/lib/lib.go\npackage hooks\nfunc Value()(r int){defer func(){r++}();return 0}\n"}
	for _, entry := range []string{"a", "b"} {
		files["cmd/"+entry+"/main.go"] = "package main\nimport \"example.test/app/lib\"\nfunc main(){println(lib.Value())}\n"
		files["cmd/"+entry+"/inject.go"] = registration
	}
	f := newFixture(t, files)
	out := filepath.Join(f.dir, "bin")
	if err := os.Mkdir(out, 0700); err != nil {
		t.Fatal(err)
	}
	f.cli("build", "-o", out+string(os.PathSeparator), "./cmd/a", "./cmd/b")
	f.run(filepath.Join("bin", "a"), "2")
	f.run(filepath.Join("bin", "b"), "2")
}

func TestNativeCoverageWithInjectedDependency(t *testing.T) {
	f := newFixture(t, map[string]string{
		"lib/lib.go":         "package lib\nfunc Value()int{return 1}\n",
		"lib/lib_test.go":    "package lib\nimport \"testing\"\nfunc TestValue(t *testing.T){if Value()!=2{t.Fatal(\"missing injection\")}}\n",
		"lib/inject_test.go": "//go:build goinject || generate\n\npackage lib\nimport _ \"example.test/app/hooks\"\n",
		"helper/helper.go":   "package helper\nfunc Increment(v int)int{return v+1}\n",
		"hooks/rule.go":      "//inject:example.test/app/lib/lib.go\npackage hooks\nimport \"example.test/app/helper\"\nfunc Value()(r int){defer func(){r=helper.Increment(r)}();return 0}\n",
	})
	f.cli("test", "-coverprofile=coverage.out", "-coverpkg=./lib,./helper", "./lib")
	if data, err := os.ReadFile(filepath.Join(f.dir, "coverage.out")); err != nil || len(data) == 0 {
		t.Fatalf("coverage profile: %v", err)
	}
}
