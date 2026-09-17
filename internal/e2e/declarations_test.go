package e2e

import "testing"

func TestGroupedAddedDeclarationsKeepTheirGoValues(t *testing.T) {
	f := newFixture(t, map[string]string{
		"main.go":    "package main\nimport \"example.test/app/lib\"\nfunc main(){println(lib.Value())}\n",
		"inject.go":  registration,
		"lib/lib.go": "package lib\nfunc Value()int{return 0}\n",
		"hooks/group.go": `//inject:example.test/app/lib/lib.go
package hooks
//inject:add
const (
	 First=iota+10
	 Second
	 Third
)
//inject:add
var (
	 Sum=First+Second+Third
	 Expected=Sum+9
)
func Value()(out int){defer func(){out=Expected}();return 0}
`,
	})
	name := "groups-app" + exeSuffix()
	f.cli("build", "-o", name, ".")
	f.run(name, "42")
}

func TestPackageLevelTargetMatchesDeclarationInAnyFile(t *testing.T) {
	f := newFixture(t, map[string]string{
		"main.go":      "package main\nimport \"example.test/app/lib\"\nfunc main(){println(lib.Sum())}\n",
		"inject.go":    registration,
		"lib/lib.go":   "package lib\nfunc Sum()int{return 0}\n",
		"lib/other.go": "package lib\nfunc unused(){}\n",
		"hooks/pkg.go": `//inject:example.test/app/lib
package hooks
func Sum()(out int){defer func(){out=42}();return 0}
`,
	})
	name := "package-target-app" + exeSuffix()
	f.cli("build", "-o", name, ".")
	f.run(name, "42")
}

func TestSelectedVariantOwnsItsDeclarationRoutedMainInit(t *testing.T) {
	f := proxyFixture(t)
	f.write("hooks/selected.go", variant(">=v1.0.0 <v2.0.0", `import target "example.test/library"
//inject:add
func init(){println("target-init")}
//inject:add
//inject:main
func init(){println("main-init",target.Value())}
func Value()(out int){defer func(){out+=3}();return 0}`))
	f.write("hooks/unselected.go", variant(">=v2.0.0", `//inject:add
//inject:main
func init(){panic("unselected declaration-main leaked")}
func Value()(out int){defer func(){out+=1000}();return 0}`))
	name := "declaration-main-app" + exeSuffix()
	f.cli("build", "-o", name, ".")
	// The package initializer must run before entry main initialization, and the
	// routed initializer calls the instrumented target through its real import.
	f.run(name, "target-init\nmain-init 5\n5")
	f.cli("build", "-o", name, ".")
	f.run(name, "target-init\nmain-init 5\n5")
	f.fails("vendor does not support entry initialization", "vendor", ".")
}
