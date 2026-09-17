package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalModuleReplacement(t *testing.T) {
	f := newFixture(t, map[string]string{
		"go.mod":             "module example.test/app\ngo 1.25.0\nrequire example.test/library v1.0.0\nreplace example.test/library => ./library\n",
		"main.go":            "package main\nimport \"example.test/library\"\nfunc main(){println(library.Value())}\n",
		"inject.go":          registration,
		"library/go.mod":     "module example.test/library\ngo 1.25.0\n",
		"library/library.go": "package library\nfunc Value()int{return 2}\n",
		"hooks/hook.go": `//inject:example.test/library/library.go
package hooks
func Value()(out int){defer func(){out+=3}();return 0}
`,
	})
	name := "replace-app" + exeSuffix()
	f.cli("build", "-o", name, ".")
	f.run(name, "5")
}

func TestWorkspaceResolvesApplicationLibraryAndRuleModules(t *testing.T) {
	f := newFixture(t, map[string]string{
		"go.work":            "go 1.25.0\nuse (\n./app\n./library\n./rules\n)\n",
		"app/go.mod":         "module example.test/app\ngo 1.25.0\nrequire (\nexample.test/library v1.0.0\nexample.test/rules v1.0.0\n)\n",
		"app/main.go":        "package main\nimport \"example.test/library\"\nfunc main(){println(library.Value())}\n",
		"app/inject.go":      "//go:build goinject\n\npackage main\nimport _ \"example.test/rules\"\n",
		"library/go.mod":     "module example.test/library\ngo 1.25.0\n",
		"library/library.go": "package library\nfunc Value()int{return 2}\n",
		"rules/go.mod":       "module example.test/rules\ngo 1.25.0\n",
		"rules/hook.go": `//inject:example.test/library/library.go
package rules
func Value()(out int){defer func(){out*=3}();return 0}
`,
	})
	f.env["GOWORK"] = filepath.Join(f.dir, "go.work")
	name := "workspace-app" + exeSuffix()
	f.cli("build", "-o", name, "./app")
	f.run(name, "6")
}

func TestCgoPackageCanInjectItsPureGoFunction(t *testing.T) {
	f := newFixture(t, map[string]string{
		"main.go":    "package main\nimport \"example.test/app/lib\"\nfunc main(){println(lib.Value())}\n",
		"inject.go":  registration,
		"lib/lib.go": "package lib\nfunc Value()int{return nativeValue()}\n",
		"lib/cgo.go": `package lib
/*
static int native_value(void) { return 2; }
*/
import "C"
func nativeValue()int{return int(C.native_value())}
`,
		"hooks/hook.go": `//inject:example.test/app/lib/lib.go
package hooks
func Value()(out int){defer func(){out+=3}();return 0}
`,
	})
	if strings.TrimSpace(f.goCommand("env", "CGO_ENABLED")) != "1" {
		t.Skip("CGO_ENABLED is not 1; this scenario requires an enabled native C toolchain")
	}
	name := "cgo-app" + exeSuffix()
	f.cli("build", "-o", name, ".")
	f.run(name, "5")
}
