package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestColdHotCacheAndTemplateOnlyInvalidation(t *testing.T) {
	files := map[string]string{
		"main.go":    "package main\nimport \"example.test/app/lib\"\nfunc main(){println(lib.Value())}\n",
		"inject.go":  registration,
		"lib/lib.go": "package lib\nfunc Value()string{return \"base\"}\n",
	}
	hook := func(value string) string {
		return fmt.Sprintf(`//inject:example.test/app/lib/lib.go
package hooks
func Value()(out string){defer func(){out+=%q}();return ""}
`, value)
	}
	files["hooks/hook.go"] = hook("-first")
	f := newFixture(t, files)
	f.env["GOCACHE"] = filepath.Join(f.dir, "cold-cache")
	name := "cache-app" + exeSuffix()
	cold := f.inspect(f.cli("build", "-work", "-o", name, "."))
	f.run(name, "base-first")
	hot := f.inspect(f.cli("build", "-work", "-o", name, "."))
	f.run(name, "base-first")
	if cold.Fingerprint != hot.Fingerprint {
		t.Fatalf("identical inputs changed fingerprint: %s != %s", cold.Fingerprint, hot.Fingerprint)
	}
	f.write("hooks/hook.go", hook("-second"))
	changed := f.inspect(f.cli("build", "-work", "-o", name, "."))
	f.run(name, "base-second")
	if changed.Fingerprint == hot.Fingerprint {
		t.Fatal("template-only change did not invalidate the build identity")
	}
	var count int
	for _, pkg := range hot.Packages {
		if pkg.Package == "example.test/app/lib" {
			count += len(pkg.Result.Matches)
		}
	}
	if count != 1 {
		t.Fatalf("hot cache lost the injection evidence or duplicated it: %d matches", count)
	}
}

func TestConcurrentEntrypointsKeepDifferentOrderIsolated(t *testing.T) {
	files := map[string]string{"lib/lib.go": "package lib\nfunc Value()string{return \"base\"}\n"}
	for _, entry := range []string{"a", "b"} {
		files["cmd/"+entry+"/main.go"] = "package main\nimport \"example.test/app/lib\"\nfunc main(){println(lib.Value())}\n"
		files["cmd/"+entry+"/inject.go"] = "//go:build goinject\n\npackage main\nimport _ \"example.test/app/hooks/" + entry + "\"\n"
		first, second := 10, 20
		if entry == "b" {
			first, second = 20, 10
		}
		for _, hook := range []struct {
			name, value string
			order       int
		}{{"first", "A", first}, {"second", "B", second}} {
			files["hooks/"+entry+"/"+hook.name+".go"] = fmt.Sprintf(`//inject:example.test/app/lib/lib.go
package hooks
//inject:order %d
func Value()(out string){defer func(){out+=%q}();return ""}
`, hook.order, hook.value)
		}
	}
	f := newFixture(t, files)
	type result struct {
		entry, output string
		err           error
	}
	completed := make(chan result, 2)
	for _, entry := range []string{"a", "b"} {
		go func(entry string) {
			output, err := invoke(f.dir, f.env, 6*time.Minute, "build", "-o", entry+exeSuffix(), "./cmd/"+entry)
			completed <- result{entry, output, err}
		}(entry)
	}
	var failures []result
	for i := 0; i < 2; i++ {
		result := <-completed
		if result.err != nil {
			failures = append(failures, result)
		}
	}
	for _, failure := range failures {
		t.Errorf("entry %s concurrent build: %v\n%s", failure.entry, failure.err, failure.output)
	}
	if len(failures) > 0 {
		return
	}
	f.run("a"+exeSuffix(), "baseBA")
	f.run("b"+exeSuffix(), "baseAB")
	// Reusing entry A after B also must not pick up B's cached rule order.
	f.cli("build", "-o", "a"+exeSuffix(), "./cmd/a")
	f.run("a"+exeSuffix(), "baseBA")
}
