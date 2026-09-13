package e2e

import "testing"

// These are general agent-building primitives: opaque goroutine state,
// immutable snapshot callbacks and verified bridges into added runtime code.
// The fixture is deliberately independent of a tracing vendor or exporter.
func TestOpaqueContextSnapshotsAndAddedRuntimeBridges(t *testing.T) {
	f := newFixture(t, map[string]string{
		"main.go": `package main
import("fmt";"example.test/app/api")
type state struct{value int}
func(s *state) GoInjectSnapshot()any{return &state{value:s.value}}
func main(){
 parent:=&state{value:73};api.Set(parent)
 done:=make(chan string,1)
 go func(){child:=api.Get().(*state);if child==parent{panic("shared mutable context")};child.value=99
  nested:=make(chan int,1);go func(){nested<-api.Get().(*state).value}()
  done<-fmt.Sprintf("%d:%d",child.value,<-nested)
 }()
 fmt.Printf("%s:%d",<-done,api.Get().(*state).value)
}
`,
		"inject.go":  registration,
		"api/api.go": "package api\nfunc Get()any{return nil}\nfunc Set(v any){}\n",
		"hooks/runtime.go": `//inject:runtime/runtime2.go
package hooks
import _ "unsafe"
type g struct{
 //inject:add
 goInjectContext any
}
//inject:add
//go:linkname GoInjectContextGet
//go:nosplit
func GoInjectContextGet()any{return getg().m.curg.goInjectContext}
//inject:add
//go:linkname GoInjectContextSet
//go:nosplit
func GoInjectContextSet(v any){getg().m.curg.goInjectContext=v}
`,
		"hooks/proc.go": `//inject:runtime/proc.go
package hooks
func newproc1(fn *funcval,parent *g,callerpc uintptr,parked bool,reason waitReason)(child *g){
 defer func(){
  value:=parent.goInjectContext
  if snapshot,ok:=value.(interface{GoInjectSnapshot()any});ok{value=snapshot.GoInjectSnapshot()}
  child.goInjectContext=value
 }()
 return nil
}
`,
		"hooks/api.go": `//inject:example.test/app/api/api.go
package hooks
import _ "unsafe"
//inject:add
//go:linkname read runtime.GoInjectContextGet
func read()any
//inject:add
//go:linkname write runtime.GoInjectContextSet
func write(v any)
func Get()(value any){defer func(){value=read()}();return nil}
func Set(value any){write(value)}
`,
	})
	f.cli("build", "-o", "context-app"+exeSuffix(), ".")
	f.run("context-app", "99:99:73")
	f.fails("does not support", "vendor", ".")
}
