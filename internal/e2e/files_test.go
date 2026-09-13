package e2e

import "testing"

func TestExplicitGoFilesPreserveEntryAndInjectedDependencies(t *testing.T) {
	f := newFixture(t, map[string]string{
		"main.go": `package main
import "fmt"
func main() { fmt.Println("body") }
`,
		"ignored.go": `package main
func main() { panic("file list must not compile the whole directory") }
`,
		"inject.go": registration,
		"hooks/main.go": `//inject:main
package hooks
import "example.test/app/helper"
func main() { helper.Print() }
`,
		"helper/helper.go": `package helper
import "fmt"
func Print() { fmt.Println("hook") }
`,
		"main_test.go": `package main
import "testing"
func TestMainFile(t *testing.T) { main() }
`,
	})
	f.inspect(f.cli("build", "-work", "-o", "app"+exeSuffix(), "main.go"))
	f.run("app", "hook\nbody")
	f.cli("test", "-v", "main.go", "main_test.go")
}
