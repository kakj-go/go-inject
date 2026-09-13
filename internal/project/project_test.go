package project

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	t.Setenv("GOENV", "off")
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOPACKAGESDRIVER", "off")
	t.Setenv("GOTOOLCHAIN", runtime.Version())
	dir := t.TempDir()
	writeFixture(t, filepath.Join(dir, "go.mod"), "module example.com/app\ngo 1.26.0\n")
	writeFixture(t, filepath.Join(dir, "main.go"), "package main\nfunc main() {}\n")
	return dir
}

func writeFixture(t *testing.T, filename, source string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSplitFlagsConsumesValuesAndPreservesTail(t *testing.T) {
	args := []string{"-vet", "off", "-exec", "emulator --arg", "-skip", "TestSlow", "./...", "-test.run", "goinject", "-args", "-custom", "payload", "-tags=goinject"}
	flags, roots, err := SplitFlags(args)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(roots, []string{"./..."}) {
		t.Fatalf("roots = %q", roots)
	}
	if !reflect.DeepEqual(flags, append(append([]string{}, args[:6]...), args[7:]...)) {
		t.Fatalf("flags = %q", flags)
	}
	if _, _, err := SplitFlags([]string{"-unknown", "not-a-package"}); err == nil {
		t.Fatal("unknown flag accepted")
	}
	if _, _, err := SplitFlags([]string{"-vet"}); err == nil {
		t.Fatal("missing value accepted")
	}
}

func TestFlagsAreTokenAwareAndLastTagsWin(t *testing.T) {
	flags, _, err := SplitFlags([]string{"-tags=discard", "-tags", "final,second", "-o", "-work", "-work=false", "-run=goinject", "-args", "-tags=goinject"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(Tags(flags), []string{"final", "second"}) {
		t.Fatalf("tags = %q", Tags(flags))
	}
	if Has(flags, "work") {
		t.Fatal("flag value or disabled flag treated as enabled -work")
	}
	if value, ok := Value(flags, "o"); !ok || value != "-work" {
		t.Fatalf("output = %q %v", value, ok)
	}
	if got := ListFlags(flags); !reflect.DeepEqual(got, []string{"-tags=discard", "-tags", "final,second"}) {
		t.Fatalf("list flags = %q", got)
	}
	removed := Remove(flags, "work", false)
	if value, _ := Value(removed, "o"); value != "-work" {
		t.Fatal("Remove modified another flag's value")
	}
	if !slices.Contains(removed, "-tags=goinject") {
		t.Fatal("Remove changed opaque test arguments")
	}
	if _, _, err := SplitFlags([]string{"-tags=goinject", "-tags=ordinary"}); err != nil {
		t.Fatalf("overridden tags rejected: %v", err)
	}
	if _, _, err := SplitFlags([]string{"-tags=ordinary,goinject"}); err == nil {
		t.Fatal("effective registration tag accepted")
	}
}

func TestResolveArgsChdirPersistentFlagsAndOverrides(t *testing.T) {
	dir := fixture(t)
	child := filepath.Join(dir, "child")
	writeFixture(t, filepath.Join(child, "go.mod"), "module example.com/child\ngo 1.26.0\n")
	goenv := filepath.Join(dir, "goenv")
	writeFixture(t, goenv, "GOFLAGS=-tags=persisted -mod=readonly\n")
	t.Setenv("GOENV", goenv)
	resolved, flags, roots, err := ResolveArgs(context.Background(), dir, []string{"-C", "child", "-tags=command", "./..."})
	if err != nil {
		t.Fatal(err)
	}
	if resolved != canonicalDirectory(child) || !reflect.DeepEqual(roots, []string{"./..."}) {
		t.Fatalf("resolved = %q %q", resolved, roots)
	}
	if Has(flags, "C") || !reflect.DeepEqual(Tags(flags), []string{"command"}) {
		t.Fatalf("flags = %q", flags)
	}
	if mode, ok := Value(flags, "mod"); !ok || mode != "readonly" {
		t.Fatalf("GOFLAGS not captured: %q", flags)
	}
	if _, _, _, err := ResolveArgs(context.Background(), dir, []string{"-v", "-C", "child"}); err == nil {
		t.Fatal("misplaced -C accepted")
	}
	t.Setenv("GOFLAGS", "-toolexec=unwanted-command")
	if _, _, _, err := ResolveArgs(context.Background(), dir, nil); err == nil || !strings.Contains(err.Error(), "managed") {
		t.Fatalf("GOFLAGS tool override = %v", err)
	}
}

func TestQuotedFlagsUseGoSyntax(t *testing.T) {
	got, err := splitQuoted(`'-gcflags=all=-N -l' -tags=a,b "-ldflags=-X=main.value=hello world"`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-gcflags=all=-N -l", "-tags=a,b", "-ldflags=-X=main.value=hello world"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("quoted = %q", got)
	}
	if _, err := splitQuoted(`'-tags=unterminated`); err == nil {
		t.Fatal("unterminated quote accepted")
	}
}

func TestEnvironmentUsesTargetToolContext(t *testing.T) {
	dir := fixture(t)
	t.Setenv("GOOS", "linux")
	t.Setenv("GOARCH", "arm64")
	t.Setenv("GOARM64", "v8.1")
	t.Setenv("GOEXPERIMENT", "none")
	t.Setenv("CGO_ENABLED", "0")
	e, err := Environment(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	c := e.Context([]string{"-tags=first", "-tags=last", "-race"}, true)
	if c.GOOS != "linux" || c.GOARCH != "arm64" || c.CgoEnabled {
		t.Fatalf("context = %+v", c)
	}
	if !slices.Contains(c.ToolTags, "arm64.v8.1") || !slices.Contains(c.ToolTags, "race") || slices.Contains(c.ToolTags, "amd64.v1") || slices.Contains(c.ToolTags, "goexperiment.jsonv2") {
		t.Fatalf("tool tags = %q", c.ToolTags)
	}
	if !reflect.DeepEqual(c.BuildTags, []string{"last", "goinject"}) {
		t.Fatalf("build tags = %q", c.BuildTags)
	}
	if !slices.Contains(c.ReleaseTags, "go1.26") {
		t.Fatalf("release tags = %q", c.ReleaseTags)
	}
	if c.Compiler != "gc" {
		t.Fatalf("compiler = %q", c.Compiler)
	}
}

func TestMetadataKeepsGoPackageNameAndSkipsUnusedImports(t *testing.T) {
	dir := fixture(t)
	writeFixture(t, filepath.Join(dir, "go.mod"), "module example.com/app\ngo 1.26.0\nrequire example.com/hooks v1.0.0\nreplace example.com/hooks => ./hooks\n")
	writeFixture(t, filepath.Join(dir, "hooks", "go.mod"), "module example.com/hooks\ngo 1.26.0\n")
	writeFixture(t, filepath.Join(dir, "hooks", "v3", "rule.go"), "package actualname\nimport _ \"missing.example/unselected\"\nfunc F() {}\n")
	p, err := Metadata(context.Background(), dir, nil, "example.com/hooks/v3")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "actualname" || p.Module == nil || p.Module.Path != "example.com/hooks" || p.Module.Replace == nil {
		t.Fatalf("metadata = %+v", p)
	}
	if p.Dir != canonicalDirectory(filepath.Join(dir, "hooks", "v3")) {
		t.Fatalf("directory = %q", p.Dir)
	}
	for _, bad := range []string{"-toolexec=command", "example.com/hooks/...", "./hooks", "example.com/hooks@v1.0.0"} {
		if _, err := Metadata(context.Background(), dir, nil, bad); err == nil {
			t.Fatalf("accepted metadata path %q", bad)
		}
	}
	if _, err := List(context.Background(), dir, nil, false, false, "-toolexec=command"); err == nil {
		t.Fatal("package flag injection accepted")
	}
}

func TestMetadataRespectsVendorAndWorkspace(t *testing.T) {
	dir := fixture(t)
	writeFixture(t, filepath.Join(dir, "go.mod"), "module example.com/app\ngo 1.26.0\nrequire example.com/provider v1.0.0\nreplace example.com/provider => ./provider\n")
	writeFixture(t, filepath.Join(dir, "provider", "go.mod"), "module example.com/provider\ngo 1.26.0\n")
	writeFixture(t, filepath.Join(dir, "provider", "rules", "rule.go"), "package actualname\nfunc F() {}\n")
	writeFixture(t, filepath.Join(dir, "goinject.go"), "//go:build goinject\n\npackage main\nimport _ \"example.com/provider/rules\"\n")
	if output, err := Command(context.Background(), dir, "mod", "vendor").CombinedOutput(); err != nil {
		t.Fatalf("vendor: %s: %v", output, err)
	}
	p, err := Metadata(context.Background(), dir, []string{"-mod=vendor"}, "example.com/provider/rules")
	if err != nil {
		t.Fatal(err)
	}
	if p.Dir != canonicalDirectory(filepath.Join(dir, "vendor", "example.com", "provider", "rules")) || p.Name != "actualname" {
		t.Fatalf("vendor metadata = %+v", p)
	}
	workfile := filepath.Join(dir, "go.work")
	writeFixture(t, workfile, "go 1.26.0\nuse (\n.\n./provider\n)\n")
	t.Setenv("GOWORK", workfile)
	p, err = Metadata(context.Background(), dir, []string{"-mod=readonly"}, "example.com/provider/rules")
	if err != nil {
		t.Fatal(err)
	}
	if p.Dir != canonicalDirectory(filepath.Join(dir, "provider", "rules")) || p.Module == nil || !p.Module.Main || p.Module.Path != "example.com/provider" {
		t.Fatalf("workspace metadata = %+v", p)
	}
}
