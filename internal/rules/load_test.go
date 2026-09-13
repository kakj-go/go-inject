package rules

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kakj-go/go-inject/internal/project"
)

type ruleFixture struct {
	dir      string
	env      project.Env
	root     *project.Package
	business map[string]*project.Package
}

func putRule(t *testing.T, dir, name, source string) {
	t.Helper()
	filename := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newRulesFixture(t *testing.T, imports string) ruleFixture {
	t.Helper()
	for name, value := range map[string]string{"GOENV": "off", "GOFLAGS": "", "GOWORK": "off", "GOPROXY": "off", "GOSUMDB": "off", "GOPACKAGESDRIVER": "off", "GOTOOLCHAIN": runtime.Version()} {
		t.Setenv(name, value)
	}
	dir := t.TempDir()
	putRule(t, dir, "go.mod", "module example.com/app\ngo 1.26.0\nrequire example.com/provider v1.0.0\nreplace example.com/provider => ./provider\n")
	putRule(t, dir, "main.go", "package main\nfunc main() {}\n")
	putRule(t, dir, "goinject.go", "//go:build goinject\n\npackage main\nimport (\n"+imports+"\n)\n")
	putRule(t, dir, "provider/go.mod", "module example.com/provider\ngo 1.26.0\n")
	e, err := project.Environment(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	roots, err := project.List(context.Background(), dir, nil, false, false, ".")
	if err != nil || len(roots) != 1 {
		t.Fatalf("roots: %v %v", roots, err)
	}
	return ruleFixture{dir: dir, env: e, root: roots[0], business: map[string]*project.Package{
		"example.com/library": {ImportPath: "example.com/library", Name: "differentname", Module: &project.Module{Path: "example.com/library", Version: "v1.9.0"}},
	}}
}

func (f ruleFixture) load() (*Set, error) {
	return Load(context.Background(), f.env, f.root, f.business, nil, false)
}

func TestLoadWholeFileVariantAndDeduplicateAggregates(t *testing.T) {
	f := newRulesFixture(t, `_ "example.com/provider/all"
_ "example.com/provider/new"`)
	putRule(t, f.dir, "provider/all/goinject.go", "//go:build goinject\n\npackage aggregator\nimport (\n_ \"example.com/provider/old\"\n_ \"example.com/provider/new\"\n)\n")
	putRule(t, f.dir, "provider/old/rule.go", "//inject:example.com/library/source.go\n//inject:id shared\n//inject:version <v1.8.0\npackage old\nimport _ \"missing.example/unused\"\nfunc unparseable syntax\n")
	selected := "//inject:example.com/library/source.go\n//inject:id shared\n//inject:version >=v1.8.0 <v2.0.0\npackage newname\n//inject:add\nvar selectedHelper = 1\n//inject:order 10\nfunc F() {}\n"
	putRule(t, f.dir, "provider/new/rule.go", selected)
	set, err := f.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != 1 || string(set.Files[0].Source) != selected {
		t.Fatalf("wrong whole-file selection: %+v", set.Files)
	}
	if len(set.Statuses) != 1 || set.Statuses[0].State != "selected" {
		t.Fatalf("statuses: %+v", set.Statuses)
	}
	if set.Packages["example.com/provider/new"].Name != "newname" {
		t.Fatal("provider name guessed from import path")
	}
	if !strings.HasPrefix(set.Files[0].ID, "example.com/provider:shared:") {
		t.Fatalf("ID = %s", set.Files[0].ID)
	}
}

func TestUnapplicableTargetsDoNotEvaluateUnknownVersion(t *testing.T) {
	f := newRulesFixture(t, `_ "example.com/provider/hooks"`)
	putRule(t, f.dir, "provider/hooks/rule.go", "//inject:example.com/absent/source.go\n//inject:id absent\n//inject:version >=v1.0.0\npackage hooks\nfunc F() {}\n")
	set, err := f.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != 0 || len(set.Statuses) != 1 || set.Statuses[0].State != "not-applicable" {
		t.Fatalf("set=%+v", set)
	}
}

func TestVersionFailuresRemainStrict(t *testing.T) {
	for _, failure := range []string{"no-version", "no-match", "overlap", "standard"} {
		t.Run(failure, func(t *testing.T) {
			f := newRulesFixture(t, `_ "example.com/provider/one"
_ "example.com/provider/two"`)
			condition1, condition2 := "<v1.8.0", ">=v1.8.0 <v2.0.0"
			switch failure {
			case "no-version":
				f.business["example.com/library"].Module.Replace = &project.Module{Dir: "local"}
			case "no-match":
				f.business["example.com/library"].Module.Version = "v2.1.0"
			case "overlap":
				condition1 = ">=v1.0.0"
			case "standard":
				f.business["example.com/library"].Standard = true
			}
			for name, condition := range map[string]string{"one": condition1, "two": condition2} {
				putRule(t, f.dir, "provider/"+name+"/rule.go", "//inject:example.com/library/source.go\n//inject:id same\n//inject:version "+condition+"\npackage hooks\nfunc F() {}\n")
			}
			if _, err := f.load(); err == nil {
				t.Fatalf("%s incorrectly succeeded", failure)
			}
		})
	}
}

func TestGoExcludedVariantHeadersRemainVisible(t *testing.T) {
	f := newRulesFixture(t, `_ "example.com/provider/current"
_ "example.com/provider/future"`)
	putRule(t, f.dir, "provider/current/rule.go", "//inject:example.com/library/source.go\n//inject:id release\npackage current\nfunc F() {}\n")
	putRule(t, f.dir, "provider/future/rule.go", "//go:build go1.99\n\n//inject:example.com/library/source.go\n//inject:id release\npackage future\nfunc newer Go syntax need not parse\n")
	set, err := f.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != 1 || !strings.Contains(set.Files[0].Provider, "current") {
		t.Fatalf("set=%+v", set)
	}
	putRule(t, f.dir, "goinject.go", "//go:build goinject\n\npackage main\nimport _ \"example.com/provider/future\"\n")
	if _, err := f.load(); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("missing Go variant=%v", err)
	}
}

func TestRegistrationDoesNotFollowRuntimeImports(t *testing.T) {
	f := newRulesFixture(t, `_ "example.com/provider/one"`)
	putRule(t, f.dir, "provider/one/rule.go", "//inject:example.com/library/source.go\npackage hooks\nimport _ \"example.com/provider/two\"\nfunc F() {}\n")
	putRule(t, f.dir, "provider/two/rule.go", "//inject:example.com/library/source.go\npackage runtimehelper\nfunc G() {}\n")
	set, err := f.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != 1 || set.Packages["example.com/provider/two"] != nil {
		t.Fatalf("runtime import enabled a rule: %+v", set)
	}
}

func TestRegistrationPackageNameAndLiteralImports(t *testing.T) {
	for _, bad := range []string{"wrong-package", "flag-import", "pattern-import", "declaration"} {
		t.Run(bad, func(t *testing.T) {
			f := newRulesFixture(t, `_ "example.com/provider/one"`)
			putRule(t, f.dir, "provider/one/rule.go", "//inject:example.com/library/source.go\npackage hooks\nfunc F() {}\n")
			src := "//go:build goinject\n\npackage main\nimport _ \"example.com/provider/one\"\n"
			switch bad {
			case "wrong-package":
				src = strings.Replace(src, "package main", "package other", 1)
			case "flag-import":
				src = strings.Replace(src, "example.com/provider/one", "-toolexec=arbitrary", 1)
			case "pattern-import":
				src = strings.Replace(src, "example.com/provider/one", "example.com/provider/...", 1)
			case "declaration":
				src += "func init() {}\n"
			}
			putRule(t, f.dir, "goinject.go", src)
			if _, err := f.load(); err == nil {
				t.Fatalf("accepted %s", bad)
			}
		})
	}
}

func TestRegistrationImportCycleIsRejected(t *testing.T) {
	f := newRulesFixture(t, `_ "example.com/provider/one"`)
	putRule(t, f.dir, "provider/one/goinject.go", "//go:build goinject\n\npackage one\nimport _ \"example.com/provider/two\"\n")
	putRule(t, f.dir, "provider/two/goinject.go", "//go:build goinject\n\npackage two\nimport _ \"example.com/provider/one\"\n")
	if _, err := f.load(); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle = %v", err)
	}
}
