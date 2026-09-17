package project

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceAliasesUseConsistentDirectoryAndEnvironment(t *testing.T) {
	dir := fixture(t)
	writeFixture(t, filepath.Join(dir, "app", "go.mod"), "module example.com/application\ngo 1.25.0\n")
	writeFixture(t, filepath.Join(dir, "app", "main.go"), "package main\nimport _ \"example.com/provider/rules\"\nfunc main() {}\n")
	writeFixture(t, filepath.Join(dir, "provider", "go.mod"), "module example.com/provider\ngo 1.25.0\n")
	writeFixture(t, filepath.Join(dir, "provider", "rules", "rules.go"), "package actualrules\n")
	writeFixture(t, filepath.Join(dir, "go.work"), "go 1.25.0\nuse (\n./app\n./provider\n)\n")
	alias := filepath.Join(t.TempDir(), "workspace-alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	workAlias := filepath.Join(alias, "go.work")
	t.Setenv("GOWORK", workAlias)
	canonical := canonicalDirectory(dir)
	for _, cwd := range []string{dir, alias} {
		command := Command(context.Background(), cwd, "env", "GOWORK")
		if command.Dir != canonical {
			t.Fatalf("command cwd = %q, want %q", command.Dir, canonical)
		}
		for _, item := range command.Environ() {
			if strings.HasPrefix(item, "GOWORK=") && item != "GOWORK="+filepath.Join(canonical, "go.work") {
				t.Fatalf("inconsistent workspace environment: %q", item)
			}
		}
		env, err := Environment(context.Background(), cwd)
		if err != nil {
			t.Fatal(err)
		}
		if env.Dir != canonical || env.GOWORK != filepath.Join(canonical, "go.work") {
			t.Fatalf("environment = %+v", env)
		}
		roots, err := List(context.Background(), env.Dir, nil, false, false, "./app")
		if err != nil || len(roots) != 1 || roots[0].Error != nil {
			t.Fatalf("workspace entry: %+v, %v", roots, err)
		}
		p, err := Metadata(context.Background(), cwd, nil, "example.com/provider/rules")
		if err != nil {
			t.Fatal(err)
		}
		if p.Name != "actualrules" || p.Module == nil || !p.Module.Main || p.Dir != filepath.Join(canonical, "provider", "rules") {
			t.Fatalf("workspace provider: %+v", p)
		}
	}
	if os.Getenv("GOWORK") != workAlias {
		t.Fatal("Command mutated the parent environment")
	}
}

func TestSourceImportsUsesActualStandardLibraryImportMap(t *testing.T) {
	dir := fixture(t)
	pkgs, err := List(context.Background(), dir, nil, true, false, "net/http")
	if err != nil {
		t.Fatal(err)
	}
	index := Index(pkgs)
	http := index["net/http"]
	const raw = "golang.org/x/net/http/httpguts"
	if http == nil || !http.Standard || http.ImportMap[raw] == "" {
		t.Fatalf("missing standard-library import map: %+v", http)
	}
	canonical := CanonicalImport(http, raw)
	if !strings.HasPrefix(canonical, "vendor/") || index[canonical] == nil {
		t.Fatalf("canonical import %q missing from Go dependency graph", canonical)
	}
	names := make(map[string]string)
	for key, pkg := range index {
		names[key] = pkg.Name
	}
	names[raw] = "applicationPackageName"
	adapted := SourceImports(http, names)
	if adapted[raw] != index[canonical].Name {
		t.Fatalf("source import name = %q, want %q", adapted[raw], index[canonical].Name)
	}
	if names[raw] != "applicationPackageName" {
		t.Fatal("SourceImports mutated the global names map")
	}
	if CanonicalImport(http, "new.example/helper") != "new.example/helper" {
		t.Fatal("unmapped import was guessed to be vendored")
	}
}

func TestSourceImportAliasesDoNotBorrowUnrelatedPackageNames(t *testing.T) {
	p := &Package{ImportMap: map[string]string{"example/pkg": "vendor/example/pkg"}}
	if _, ok := SourceImports(p, map[string]string{"example/pkg": "unrelated"})["example/pkg"]; ok {
		t.Fatal("unresolved vendor import used an application package name")
	}
	p.ImportMap["example/pkg"] = "example/pkg [example/pkg.test]"
	if name := SourceImports(p, map[string]string{"example/pkg": "declaredName"})["example/pkg"]; name != "declaredName" {
		t.Fatalf("test variant name = %q", name)
	}
}
