package e2e

import "testing"

func TestNativeLinkClosureAndMainInitExactlyOnce(t *testing.T) {
	f := newFixture(t, map[string]string{
		"main.go": `package main
import("fmt";"example.test/app/lib")
func main(){fmt.Println(lib.Value())}
`,
		"inject.go": registration,
		"lib/lib.go": `package lib
func Value()string{return "base"}
`,
		"helper/helper.go": `package helper
import("fmt";"example.test/app/state")
func Value()string{return fmt.Sprintf("linked:%d",state.Count())}
`,
		"state/state.go": `package state
var count int
func Initialize(){count++}
func Count()int{return count}
`,
		"hooks/bridge.go": `//inject:example.test/app/lib/lib.go
package hooks
import _ "unsafe"
//inject:add
//go:linkname bridge example.test/app/helper.Value
func bridge()string
func Value()(out string){defer func(){out=bridge()}();return ""}
`,
		"hooks/init.go": `//inject:main
package hooks
import "example.test/app/state"
//inject:add
func init(){state.Initialize()}
`,
	})
	name := "bridge-app" + exeSuffix()
	report := f.inspect(f.cli("build", "-work", "-o", name, "."))
	f.run(name, "linked:1")
	var found bool
	for _, pkg := range report.Packages {
		for _, link := range pkg.Result.Links {
			found = found || link.Symbol == "example.test/app/helper.Value"
		}
	}
	if !found {
		t.Fatal("inspection report did not retain native link dependency")
	}
	// A second build exercises the cached closure and must not multiply init.
	f.cli("build", "-o", name, ".")
	f.run(name, "linked:1")
}

func TestInjectedDependencyCycleIsRejected(t *testing.T) {
	f := newFixture(t, map[string]string{
		"main.go": `package main
import "example.test/app/lib"
func main(){println(lib.Value())}
`,
		"inject.go":        registration,
		"lib/lib.go":       "package lib\nfunc Value()int{return 1}\n",
		"helper/helper.go": "package helper\nimport \"example.test/app/lib\"\nfunc Value()int{return lib.Value()}\n",
		"hooks/cycle.go": `//inject:example.test/app/lib/lib.go
package hooks
import "example.test/app/helper"
func Value()(out int){defer func(){out=helper.Value()}();return 0}
`,
	})
	f.fails("injected import cycle", "build", ".")
}
