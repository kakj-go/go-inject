package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kakj-go/go-inject/internal/rewrite"
)

func TestInspectRefreshesPartialCompilerRecords(t *testing.T) {
	s := &Session{Dir: t.TempDir(), Cache: t.TempDir(), RootPath: "example.test/p"}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(); err != nil {
		t.Fatal(err)
	}
	// Model a compiler that wrote its transformed input but failed before a
	// successful root compilation could publish a complete snapshot.
	r := Record{Package: s.RootPath, Result: &rewrite.Result{
		Replacements: map[string][]byte{"p.go": []byte("package p\nfunc F() {}\n")},
	}}
	if err := s.writeRecord(r, false); err != nil {
		t.Fatal(err)
	}
	if err := Inspect(s.Dir, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Packages) != 1 || report.Packages[0].Package != r.Package {
		t.Fatalf("inspection omitted partial compiler records: %+v", report.Packages)
	}
	if s.SnapshotValid() {
		t.Fatal("inspection marked a failed compilation as a complete cache snapshot")
	}
}

func TestExecutionRecordsDoNotReturnPreflightSources(t *testing.T) {
	s := &Session{Dir: t.TempDir(), Cache: t.TempDir(), UseCacheSnapshots: true}
	plan := Record{Package: "p", Planning: true, Result: &rewrite.Result{Replacements: map[string][]byte{"p.go": []byte("planned")}}}
	actual := Record{Package: "p [p.test]", Result: &rewrite.Result{Replacements: map[string][]byte{"p.go": []byte("compiler input")}}}
	if e := s.writeRecord(plan, false); e != nil {
		t.Fatal(e)
	}
	if e := s.writeRecord(actual, true); e != nil {
		t.Fatal(e)
	}
	rs, e := s.readRecords(false)
	if e != nil {
		t.Fatal(e)
	}
	if len(rs) != 1 || rs[0].Planning || string(rs[0].Result.Replacements["p.go"]) != "compiler input" {
		t.Fatalf("wrong inspection records: %+v", rs)
	}
	if e = s.Complete(); e != nil {
		t.Fatal(e)
	}
	if !s.SnapshotValid() {
		t.Fatal("complete snapshot not recognized")
	}
	if e = os.WriteFile(filepath.Join(s.Cache, "records", hash([]byte(actual.Package))+".json"), []byte("corrupted"), 0600); e != nil {
		t.Fatal(e)
	}
	if s.SnapshotValid() {
		t.Fatal("corrupted snapshot accepted")
	}
}

func TestImportcfgResolvesStandardLibraryVendoring(t *testing.T) {
	p := filepath.Join(t.TempDir(), "importcfg")
	content := "importmap golang.org/x/net/http/httpguts=vendor/golang.org/x/net/http/httpguts\npackagefile vendor/golang.org/x/net/http/httpguts=C:/path with spaces/export.a\n"
	if e := os.WriteFile(p, []byte(content), 0600); e != nil {
		t.Fatal(e)
	}
	known, _, e := readImportcfg(p)
	if e != nil {
		t.Fatal(e)
	}
	if known["golang.org/x/net/http/httpguts"] != "C:/path with spaces/export.a" {
		t.Fatalf("missing canonical archive: %v", known)
	}
}

func TestMainRoutedDependenciesDoNotCreateTargetEdges(t *testing.T) {
	r := &rewrite.Result{Replacements: map[string][]byte{"p.go": []byte("package p; import \"fmt\";func F(){fmt.Println()}")}, Additions: map[string][]byte{"main/init.go": []byte("package main;import _ \"example.test/runtime\"")}}
	a := scopeImports(r, false)
	if len(a) != 1 || a[0] != "fmt" {
		t.Fatalf("target imports: %v", a)
	}
	a = scopeImports(r, true)
	if len(a) != 2 {
		t.Fatalf("main imports: %v", a)
	}
}
