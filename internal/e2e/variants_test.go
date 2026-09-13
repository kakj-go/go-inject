package e2e

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func proxyFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, map[string]string{
		"go.mod":    "module example.test/app\ngo 1.26.0\nrequire example.test/library v1.2.0\n",
		"main.go":   "package main\nimport \"example.test/library\"\nfunc main(){println(library.Value())}\n",
		"inject.go": registration,
	})
	const module = "module example.test/library\ngo 1.26.0\n"
	f.write("proxy/example.test/library/@v/list", "v1.2.0\n")
	f.write("proxy/example.test/library/@v/v1.2.0.info", `{"Version":"v1.2.0","Time":"2026-01-01T00:00:00Z"}`)
	f.write("proxy/example.test/library/@v/v1.2.0.mod", module)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, entry := range []struct{ name, data string }{{"go.mod", module}, {"library.go", "package library\nfunc Value()int{return 2}\n"}} {
		file, err := writer.Create("example.test/library@v1.2.0/" + entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = file.Write([]byte(entry.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, "proxy/example.test/library/@v/v1.2.0.zip"), archive.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	proxyPath := filepath.ToSlash(filepath.Join(f.dir, "proxy"))
	if filepath.VolumeName(f.dir) != "" {
		proxyPath = "/" + proxyPath
	}
	f.env["GOPROXY"] = (&url.URL{Scheme: "file", Path: proxyPath}).String()
	f.env["GOMODCACHE"] = filepath.Join(f.dir, "module-cache")
	f.env["GOFLAGS"] = "-modcacherw"
	f.goCommand("mod", "download", "example.test/library")
	return f
}

func variant(condition, body string) string {
	return fmt.Sprintf(`//inject:example.test/library/library.go
//inject:id value
//inject:version %s
package hooks
%s
`, condition, body)
}

func TestVersionVariantsRejectOverlapAndZeroMatches(t *testing.T) {
	for _, test := range []struct{ name, first, second, reason string }{{"overlap", ">=v1.0.0", ">=v1.1.0", "got 2"}, {"zero", ">=v2.0.0", ">=v3.0.0", "got 0"}} {
		t.Run(test.name, func(t *testing.T) {
			f := proxyFixture(t)
			f.write("hooks/first.go", variant(test.first, "func Value()(out int){return 0}"))
			f.write("hooks/second.go", variant(test.second, "func Value()(out int){return 0}"))
			f.fails(test.reason, "build", ".")
		})
	}
}

func TestVersionConstraintRejectsUnknownLocalReplacement(t *testing.T) {
	f := newFixture(t, map[string]string{
		"go.mod":             "module example.test/app\ngo 1.26.0\nrequire example.test/library v1.2.0\nreplace example.test/library => ./library\n",
		"main.go":            "package main\nimport \"example.test/library\"\nfunc main(){println(library.Value())}\n",
		"inject.go":          registration,
		"library/go.mod":     "module example.test/library\ngo 1.26.0\n",
		"library/library.go": "package library\nfunc Value()int{return 2}\n",
		"hooks/hook.go":      variant(">=v1.0.0", "func Value()(out int){return 0}"),
	})
	f.fails("version unknown", "build", ".")
}

func TestUnselectedDeclarationsAndMainInitializationCannotLeak(t *testing.T) {
	f := proxyFixture(t)
	f.write("hooks/selected.go", variant(">=v1.0.0 <v2.0.0", `//inject:add
func chosen()int{return 3}
func Value()(out int){defer func(){out+=chosen()}();return 0}`))
	f.write("hooks/unselected.go", variant(">=v2.0.0", `//inject:add
func chosen()int{return 1000}
//inject:add
func init(){panic("unselected declaration leaked")}
func Value()(out int){defer func(){out+=chosen()}();return 0}`))
	f.write("hooks/main_active.go", `//go:build !goinject_e2e_unselected

//inject:main
//inject:id startup
package hooks
//inject:add
func init(){println("selected-init")}
`)
	f.write("hooks/main_inactive.go", `//go:build goinject_e2e_unselected

//inject:main
//inject:id startup
package hooks
//inject:add
func init(){panic("unselected main leaked")}
`)
	name := "variant-app" + exeSuffix()
	f.cli("build", "-o", name, ".")
	f.run(name, "selected-init\n5")
}
