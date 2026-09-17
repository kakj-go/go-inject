package rules

import "testing"

func TestVersionConditions(t *testing.T) {
	for _, tt := range []struct {
		version, condition string
		want               bool
	}{{"v1.11.0", ">=v1.11.0 <v1.12.0", true}, {"v1.12.0", ">=v1.11.0 <v1.12.0", false}, {"v1.12.0", ">=v1.12.0 <v1.13.0", true}, {"", ">=v1.0.0", false}, {"v1.0.0-beta.1", "<v1.0.0", true}} {
		got, err := VersionMatches(tt.version, tt.condition)
		if err != nil || got != tt.want {
			t.Errorf("%+v: %v %v", tt, got, err)
		}
	}
	for _, bad := range []string{"^v1.0.0", ">=nope", "v1.0.0", "~v1.2.0"} {
		if _, err := VersionMatches("v1.0.0", bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestHeader(t *testing.T) {
	src := []byte("// Copyright\n//go:build goinject && go1.26\n\n//inject:example.com/lib/file.go\n//inject:id api\n//inject:version >=v1.0.0 <v2.0.0\npackage hooks\nfunc broken syntax")
	f, err := Header("rule.go", src)
	if err != nil {
		t.Fatal(err)
	}
	if f.Target != "example.com/lib" || f.TargetFile != "file.go" || f.ID != "api" {
		t.Fatalf("%+v", f)
	}
	for _, src := range []string{"//inject:version >=v1.0.0\npackage p", "//inject:../bad.go\npackage p", "//inject:typo\npackage p", "//inject:main\n//inject:main\npackage p"} {
		if _, err := Header("bad.go", []byte(src)); err == nil {
			t.Errorf("accepted %s", src)
		}
	}
}

func TestHeaderPackageTarget(t *testing.T) {
	f, err := Header("rule.go", []byte("//inject:github.com/gin-gonic/gin\npackage hooks\n"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Target != "github.com/gin-gonic/gin" || f.TargetFile != "" {
		t.Fatalf("%+v", f)
	}
	for _, src := range []string{"//inject:example.com/lib\n//inject:example.com/lib2\npackage p", "//inject:-bad/pkg\npackage p", "//inject:a//b\npackage p"} {
		if _, err := Header("bad.go", []byte(src)); err == nil {
			t.Errorf("accepted %s", src)
		}
	}
}

func TestHeaderDoesNotConsumeDeclarationDirectives(t *testing.T) {
	for _, directive := range []string{"//inject:order 10", "//inject:add", "//inject:main"} {
		source := []byte("//inject:example.com/lib/file.go\npackage hooks\n" + directive + "\nfunc F() {}\n")
		f, err := Header("hooks.go", source)
		if err != nil || f.Target != "example.com/lib" || f.TargetFile != "file.go" {
			t.Fatalf("declaration directive %q changed header: %+v, %v", directive, f, err)
		}
	}
}
