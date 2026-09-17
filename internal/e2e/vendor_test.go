package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func vendorFixture(t *testing.T) *fixture {
	return newFixture(t, map[string]string{
		"go.mod":        "module example.test/app\n\ngo 1.25.0\nrequire example.test/dep v1.0.0\nreplace example.test/dep => ./dep\n",
		"dep/go.mod":    "module example.test/dep\n\ngo 1.25.0\n",
		"dep/dep.go":    "package dep\nfunc Value()int{return 1}\n",
		"main.go":       "package main\nimport \"example.test/dep\"\nfunc main(){println(dep.Value())}\n",
		"inject.go":     "//go:build goinject || generate\n\npackage main\n//go:generate " + strconv.Quote(cliBinary) + " vendor .\nimport _ \"example.test/app/hooks\"\n",
		"hooks/rule.go": "//inject:example.test/dep/dep.go\npackage hooks\nfunc Value()(r int){defer func(){r++}();return 0}\n",
	})
}

func (f *fixture) generate() string {
	f.t.Helper()
	out, err := command(f.dir, f.env, 3*time.Minute, toolchainGo, "generate", ".")
	if err != nil {
		f.t.Fatalf("go generate: %v\n%s", err, out)
	}
	return out
}

func TestNativeGenerateAndPortableVendor(t *testing.T) {
	f := vendorFixture(t)
	f.generate()
	manifest := filepath.Join(f.dir, ".goinject", "vendor-state", "manifest.json")
	before, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Root string
		Plan []any
	}
	if err = json.Unmarshal(before, &state); err != nil {
		t.Fatal(err)
	}
	if state.Root != "." || len(state.Plan) != 1 {
		t.Fatalf("vendor state is not portable and attributed: %s", before)
	}
	f.generate()
	again, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(again) {
		t.Fatal("unchanged go generate rewrote vendor state")
	}
	if out, err := command(f.dir, f.env, 2*time.Minute, toolchainGo, "build", "-mod=vendor", "-o", "plain"+exeSuffix(), "."); err != nil {
		t.Fatalf("plain Go build: %v\n%s", err, out)
	}
	f.run("plain", "2")
	// A second checkout contains the vendored source, saved baseline and local
	// rules, with no reference to the first checkout's absolute directory.
	clone := newFixture(t, map[string]string{})
	err = filepath.WalkDir(f.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(f.dir, path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(rel, exeSuffix()) && exeSuffix() != "" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		clone.write(rel, string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	clone.generate()
	clone.cli("build", "-o", "native"+exeSuffix(), ".")
	clone.run("native", "2")
	clone.cli("vendor", "--restore")
	if out, err := command(clone.dir, clone.env, 2*time.Minute, toolchainGo, "build", "-mod=vendor", "-o", "restored"+exeSuffix(), "."); err != nil {
		t.Fatalf("restored build: %v\n%s", err, out)
	}
	clone.run("restored", "1")
}

func TestGenerateRejectsInvalidCodeBeforeDelivery(t *testing.T) {
	f := vendorFixture(t)
	f.write("hooks/rule.go", "//inject:example.test/dep/dep.go\npackage hooks\n//inject:add\nfunc Added()int{return \"invalid\"}\n")
	output, err := command(f.dir, f.env, 2*time.Minute, toolchainGo, "generate", ".")
	if err == nil || !strings.Contains(output, "cannot use") {
		t.Fatalf("invalid generated code was not rejected: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "vendor")); !os.IsNotExist(err) {
		t.Fatal("failed validation delivered vendor")
	}
}

func TestGenerateStateConflictDoesNotDeliverInitialVendor(t *testing.T) {
	f := vendorFixture(t)
	f.write(".goinject", "user file")
	_, err := command(f.dir, f.env, 2*time.Minute, toolchainGo, "generate", ".")
	if err == nil {
		t.Fatal("state path collision accepted")
	}
	if _, err := os.Stat(filepath.Join(f.dir, "vendor")); !os.IsNotExist(err) {
		t.Fatal("failed state preparation left vendor")
	}
}

func TestNativeMultipleEntryConflictIsExplicit(t *testing.T) {
	f := newFixture(t, map[string]string{"lib/lib.go": "package lib\nfunc Value()int{return 1}\n",
		"cmd/a/main.go":   "package main\nimport \"example.test/app/lib\"\nfunc main(){lib.Value()}\n",
		"cmd/b/main.go":   "package main\nimport \"example.test/app/lib\"\nfunc main(){lib.Value()}\n",
		"cmd/a/inject.go": registration,
		"hooks/rule.go":   "//inject:example.test/app/lib/lib.go\npackage hooks\nfunc Value()(r int){defer func(){r++}();return 0}\n"})
	f.fails("different generated source", "build", "./cmd/a", "./cmd/b")
}
