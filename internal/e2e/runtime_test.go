package e2e

import (
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeGoroutineFieldPropagation(t *testing.T) {
	if !strings.HasPrefix(runtime.Version(), "go1.26.") && !strings.HasPrefix(runtime.Version(), "go1.27.") {
		t.Skipf("runtime fixture is explicitly defined for Go 1.26/1.27, current toolchain %s", runtime.Version())
	}
	files := map[string]string{
		"main.go": `package main
import("fmt";"runtime")
func main(){
 runtime.GoInjectE2ESet(73)
 first:=make(chan uint64,1)
 second:=make(chan uint64,1)
 go func(){
  first<-runtime.GoInjectE2EGet()
  runtime.GoInjectE2ESet(99)
  go func(){second<-runtime.GoInjectE2EGet()}()
 }()
 fmt.Printf("%d:%d:%d",<-first,<-second,runtime.GoInjectE2EGet())
}
`,
		"inject.go": registration,
		"hooks/field.go": `//inject:runtime/runtime2.go
package hooks
type g struct {
 //inject:add
 goInjectE2EMarker uint64
}
//inject:add
//go:nosplit
func GoInjectE2ESet(value uint64){getg().m.curg.goInjectE2EMarker=value}
//inject:add
//go:nosplit
func GoInjectE2EGet()uint64{return getg().m.curg.goInjectE2EMarker}
`,
	}
	// The two explicit fixtures use the verified five-parameter newproc1
	// signature. Build constraints select the toolchain's implementation.
	for _, version := range []struct{ name, tag string }{{"126", "go1.26 && !go1.27"}, {"127", "go1.27"}} {
		files["hooks/proc"+version.name+".go"] = "//go:build " + version.tag + "\n\n" + `//inject:runtime/proc.go
//inject:id goroutine-propagation
package hooks
func newproc1(fn *funcval, parent *g, callerpc uintptr, parked bool, reason waitReason)(child *g){
 defer func(){child.goInjectE2EMarker=parent.goInjectE2EMarker}()
 return nil
}
`
	}
	f := newFixture(t, files)
	name := "runtime-app" + exeSuffix()
	report := f.inspect(f.cli("build", "-work", "-o", name, "."))
	f.run(name, "73:99:73")
	var field, method bool
	for _, pkg := range report.Packages {
		if pkg.Package == "runtime" {
			for _, match := range pkg.Result.Matches {
				field = field || strings.Contains(match.Function, "field g.goInjectE2EMarker")
				method = method || match.Function == "newproc1"
			}
		}
	}
	if !field || !method {
		t.Fatalf("runtime inspection must prove field and newproc1 changes, got %#v", report.Packages)
	}
	f.fails("does not support", "vendor", ".")
}
