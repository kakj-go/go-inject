package rewrite

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func source(name, code string) Source { return Source{Path: name, Data: []byte(code)} }
func rule(id, target, code string) Rule {
	return Rule{ID: id, Provider: "example.test/rules", Path: id + ".go", Target: target, Source: []byte(code)}
}

func execute(t *testing.T, sources []Source, result *Result, want string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", []byte("module example.test/target\n\ngo 1.26.0\n"))
	for _, s := range sources {
		data := s.Data
		if replacement, exists := result.Replacements[s.Path]; exists {
			data = replacement
		}
		write(s.Path, data)
	}
	for name, data := range result.Additions {
		write(name, data)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	output, err := cmd.CombinedOutput()
	if err != nil {
		for name, data := range result.Replacements {
			t.Logf("%s:\n%s", name, data)
		}
		for name, data := range result.Additions {
			t.Logf("%s:\n%s", name, data)
		}
		t.Fatalf("generated program failed: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != want {
		t.Fatalf("output = %q; want %q", output, want)
	}
}

func TestPreservesSliceResultsAndDiscardsOnlyTailReturn(t *testing.T) {
	sources := []Source{source("main.go", `package main
import "fmt"
func F(v []int) []int { return append(v,42) }
func main(){fmt.Println(F([]int{1}))}
`)}
	rules := []Rule{rule("slice", "example.test/target/main.go", `package main
func F(input []int)(result []int){
 defer func(){result=append(result,7)}()
 return []int{999}
}
`)}
	result, err := Package("example.test/target", sources, rules)
	if err != nil {
		t.Fatal(err)
	}
	execute(t, sources, result, "[1 42 7]")
}

func TestParameterBindingShadowsAndMultipleRuleOrdering(t *testing.T) {
	sources := []Source{source("main.go", `package main
import "fmt"
func F(value int)(result int){
 {value:=10;result=value}
 defer func(){result+=100}()
 return result+value
}
func main(){fmt.Println(F(1))}
`)}
	rules := []Rule{
		rule("b", "example.test/target/main.go", `package main
//inject:order 20
func F(arg int)(out int){arg+=3;defer func(){out*=2}();return 0}
`),
		rule("a", "example.test/target/main.go", `package main
//inject:order 10
func F(arg int)(out int){
 value:=2
 arg+=value
 {arg:=99;_=arg}
 defer func(){out+=5}()
 return 0
}
`),
	}
	result, err := Package("example.test/target", sources, rules)
	if err != nil {
		t.Fatal(err)
	}
	execute(t, sources, result, "237")
	if len(result.Matches) != 2 || result.Matches[0].Rule != "a" {
		t.Fatalf("unstable matches: %#v", result.Matches)
	}
}

func TestImportAliasesMultipleDeclarationsAndNoImport(t *testing.T) {
	for _, imports := range []string{"", `import f "fmt"; var _=f.Sprintf`, `import "fmt"; import "strings"; var _,_=fmt.Sprintf,strings.TrimSpace`} {
		t.Run(fmt.Sprintf("imports-%d", len(imports)), func(t *testing.T) {
			sources := []Source{source("main.go", "package main\n"+imports+"\nfunc F(){}\nfunc main(){F()}\n")}
			rules := []Rule{rule("imports", "example.test/target/main.go", `package main
import f "fmt"
func F(){
 fmt:="local"
 f.Println(fmt)
}
`)}
			result, err := Package("example.test/target", sources, rules)
			if err != nil {
				t.Fatal(err)
			}
			execute(t, sources, result, "local")
		})
	}
}

func TestFullSignatureShapesRejectMismatches(t *testing.T) {
	for _, test := range []struct{ name, left, right string }{
		{"slice", "[]int", "[]string"},
		{"map", "map[string]int", "map[string]string"},
		{"array", "[2]int", "[3]int"},
		{"channel", "<-chan int", "chan<- int"},
		{"callback", "func(int) error", "func(string) error"},
		{"variadic", "...int", "[]int"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sources := []Source{source("target.go", "package target\nfunc F(x "+test.left+"){}")}
			rules := []Rule{rule("mismatch", "example.test/target/target.go", "package target\nfunc F(x "+test.right+"){}")}
			if _, err := Package("example.test/target", sources, rules); err == nil || !strings.Contains(err.Error(), "signature mismatch") {
				t.Fatalf("want signature mismatch, got %v", err)
			}
		})
	}
}

func TestGenericFunctionsReceiversAndGroupedParameters(t *testing.T) {
	sources := []Source{source("main.go", `package main
import "fmt"
type Box[T any] struct{Value T}
func (b *Box[T]) Get(x,y int) T { return b.Value }
func F[T ~int](a,b T) T{return a+b}
func main(){box:=Box[string]{Value:"ok"};fmt.Println(box.Get(1,2),F(1,2))}
`)}
	rules := []Rule{rule("generic", "example.test/target/main.go", `package main
type Box[V any] struct{Value V}
func (box *Box[V]) Get(first,second int)(out V){first+=second;return out}
func F[V ~int](first,second V)(out V){first++;return 0}
`)}
	result, err := Package("example.test/target", sources, rules)
	if err != nil {
		t.Fatal(err)
	}
	execute(t, sources, result, "ok 4")
}

func TestAddFieldsHelpersInitializationAndCrossFileReference(t *testing.T) {
	sources := []Source{source("main.go", `package main
import "fmt"
type Target struct{value int}
func (t *Target) F() int{return t.value+t.extra}
func main(){fmt.Println((&Target{value:1}).F())}
`)}
	rules := []Rule{
		rule("field", "example.test/target/main.go", `package main
type Target struct {
 value int
 //inject:add
 extra int
}
func (obj *Target) F()(out int){obj.extra=helper();return 0}
`),
		rule("helper", "example.test/target/main.go", `package main
//inject:add
var added int
//inject:add
func init(){added=7}
//inject:add
func helper() int{return added}
`),
	}
	result, err := Package("example.test/target", sources, rules)
	if err != nil {
		t.Fatal(err)
	}
	execute(t, sources, result, "8")
	if len(result.Additions) != 1 {
		t.Fatalf("additions: %#v", result.Additions)
	}
}

func TestFailuresAreExplicitAndInputsUnmodified(t *testing.T) {
	sources := []Source{source("target.go", `package target
type T struct{x int}
func F(){}
`)}
	for _, test := range []struct{ name, target, code, reason string }{
		{"file", "example.test/target/missing.go", "package target\nfunc F(){}", "matched 0 files"},
		{"function", "example.test/target/target.go", "package target\nfunc Missing(){}", "matched 0 declarations"},
		{"type", "example.test/target/target.go", "package target\ntype Missing struct{}", "does not exist"},
		{"field", "example.test/target/target.go", "package target\ntype T struct{missing int}", "does not exist"},
		{"duplicate", "example.test/target/target.go", "package target\n//inject:add\nfunc F(){}", "already exists"},
		{"order", "example.test/target/target.go", "package target\n//inject:order invalid\nfunc F(){}", "invalid //inject:order"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := string(sources[0].Data)
			_, err := Package("example.test/target", sources, []Rule{rule(test.name, test.target, test.code)})
			if err == nil || !strings.Contains(err.Error(), test.reason) {
				t.Fatalf("want %q, got %v", test.reason, err)
			}
			if string(sources[0].Data) != before {
				t.Fatal("input changed")
			}
		})
	}
}

func TestConditionalReturnAndRecoverKeepFunctionFrame(t *testing.T) {
	sources := []Source{source("main.go", `package main
import "fmt"
func F(skip bool)(out int){defer func(){if recover()!=nil{out=7}}();panic("business")}
func main(){fmt.Println(F(true),F(false))}
`)}
	rules := []Rule{rule("recover", "example.test/target/main.go", `package main
func F(stop bool)(result int){
 defer func(){result++}()
 if stop{return 3}
 return 0
}
`)}
	result, err := Package("example.test/target", sources, rules)
	if err != nil {
		t.Fatal(err)
	}
	execute(t, sources, result, "4 8")
}

func TestNativeLinkSignatureMatchesCanonicalTarget(t *testing.T) {
	sources := []Source{source("target.go", "package target\nfunc F(){}")}
	rules := []Rule{rule("bridge", "example.test/target/target.go", `package target
import "context"
import _ "unsafe"
//inject:add
//go:linkname native remote.test/pkg.Work
func native(ctx context.Context, x ...string)(int,error)
`)}
	result, err := Package("example.test/target", sources, rules)
	if err != nil {
		t.Fatal(err)
	}
	want, err := CanonicalSignature("remote.test/pkg", []Source{source("work.go", `package pkg
import c "context"
func Work(_ c.Context, values ...string)(n int, err error){return 0,nil}
`)}, "Work")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Links) != 1 || result.Links[0].Signature != want {
		t.Fatalf("links %#v; expected %s", result.Links, want)
	}
}

func TestPreservesCompilerDirectives(t *testing.T) {
	sources := []Source{source("target.go", `//go:build !special
package target
//go:noinline
func F(){println("business")}
`)}
	result, err := Package("example.test/target", sources, []Rule{rule("directive", "example.test/target/target.go", "package target\nfunc F(){println(\"hook\")}")})
	if err != nil {
		t.Fatal(err)
	}
	generated := string(result.Replacements["target.go"])
	for _, directive := range []string{"//go:build !special", "//go:noinline", "//line target.go:"} {
		if !strings.Contains(generated, directive) {
			t.Fatalf("lost %s:\n%s", directive, generated)
		}
	}
}

func TestStableDeduplication(t *testing.T) {
	sources := []Source{source("target.go", "package target\nfunc F(){}")}
	r := rule("same", "example.test/target/target.go", "package target\nfunc F(){println(1)}")
	result, err := Package("example.test/target", sources, []Rule{r, r})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("duplicate applied: %#v", result.Matches)
	}
}

func TestSelfImportDoesNotCaptureTemplateLocals(t *testing.T) {
	sources := []Source{source("main.go", `package main
import "fmt"
func Answer()int{return 42}
func F()int{return 0}
func main(){fmt.Println(F())}
`)}
	rules := []Rule{rule("self", "example.test/target/main.go", `package main
import target "example.test/target"
func F()(out int){
 Answer:=func()int{return 99}
 _=Answer
 if true{return target.Answer()}
 return 0
}
`)}
	result, err := Package("example.test/target", sources, rules)
	if err != nil {
		t.Fatal(err)
	}
	execute(t, sources, result, "42")
}

func TestInitializerReplacementDropsUnusedImports(t *testing.T) {
	sources := []Source{source("main.go", `package main
import "strings"
var value = strings.TrimSpace(" old ")
func main(){println(value)}
`)}
	result, err := Package("example.test/target", sources, []Rule{rule("value", "example.test/target/main.go", "package main\nvar value = \"new\"")})
	if err != nil {
		t.Fatal(err)
	}
	execute(t, sources, result, "new")
}

func TestAddedGenericFieldUsesTargetParameterName(t *testing.T) {
	sources := []Source{source("main.go", `package main
type Box[T any] struct{Value T}
func main(){println(Box[int]{Added:3}.Added)}
`)}
	result, err := Package("example.test/target", sources, []Rule{rule("field", "example.test/target/main.go", `package main
type Box[V any] struct{
 //inject:add
 Added V
}
`)})
	if err != nil {
		t.Fatal(err)
	}
	execute(t, sources, result, "3")
}

func TestEquivalentBuiltinAliases(t *testing.T) {
	for _, pair := range [][2]string{{"byte", "uint8"}, {"rune", "int32"}, {"any", "interface{}"}} {
		sources := []Source{source("target.go", "package target\nfunc F(x "+pair[0]+"){}")}
		_, err := Package("example.test/target", sources, []Rule{rule("alias", "example.test/target/target.go", "package target\nfunc F(x "+pair[1]+"){}")})
		if err != nil {
			t.Errorf("%v: %v", pair, err)
		}
	}
}

func TestAuthoritativeImportNames(t *testing.T) {
	sources := []Source{source("target.go", "package target\nfunc F(){}")}
	r := rule("client", "example.test/target/target.go", `package target
import "example.test/not-the-package-name/v2"
func F(){client.Run()}
`)
	r.ImportNames = map[string]string{"example.test/not-the-package-name/v2": "client"}
	result, err := Package("example.test/target", sources, []Rule{r})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Imports) != 1 || result.Imports[0] != "example.test/not-the-package-name/v2" {
		t.Fatalf("wrong imports: %#v", result.Imports)
	}
	if strings.Contains(string(result.Replacements["target.go"]), "client.Run") {
		t.Fatal("unbound original import name")
	}
}

func TestOriginalStatementSourceLocations(t *testing.T) {
	sources := []Source{source("origin.go", `package main
import("fmt";"runtime";"path/filepath")
func F(){
 if true{a:=1;_=a}
 _, file, line, _:=runtime.Caller(0)
 fmt.Printf("%s:%d",filepath.Base(file),line)
}
func main(){F()}
`)}
	result, err := Package("example.test/target", sources, []Rule{rule("location", "example.test/target/origin.go", `package main
func F(){
 a:=1
 _=a
}
`)})
	if err != nil {
		t.Fatal(err)
	}
	execute(t, sources, result, "origin.go:5")
}

func TestPackageCanBeRewrittenConcurrently(t *testing.T) {
	sources := []Source{source("target.go", "package target\nfunc F(x int)int{return x}")}
	rules := []Rule{rule("parallel", "example.test/target/target.go", "package target\nfunc F(a int)(b int){a++;return 0}")}
	want, err := Package("example.test/target", sources, rules)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			got, err := Package("example.test/target", sources, rules)
			if err != nil {
				t.Fatal(err)
			}
			if string(got.Replacements["target.go"]) != string(want.Replacements["target.go"]) {
				t.Fatal("non-deterministic concurrent output")
			}
		})
	}
}

func TestTwoTemplatesMayUseTheSameLabel(t *testing.T) {
	sources := []Source{source("main.go", "package main\nfunc F(){}\nfunc main(){F()}")}
	rules := []Rule{
		rule("one", "example.test/target/main.go", `package main
func F(){goto Done;Done:println("one")}
`),
		rule("two", "example.test/target/main.go", `package main
func F(){goto Done;Done:println("two")}
`),
	}
	result, err := Package("example.test/target", sources, rules)
	if err != nil {
		t.Fatal(err)
	}
	execute(t, sources, result, "one\ntwo")
}

func TestMainInitializerUsesRoutedAddition(t *testing.T) {
	sources := []Source{source("target.go", "package target\nfunc F(){}")}
	result, err := Package("example.test/target", sources, []Rule{rule("main-init", "main", `package main
//inject:add
func init(){println("ready")}
`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Additions) != 1 {
		t.Fatalf("unexpected additions: %#v", result.Additions)
	}
	for path, data := range result.Additions {
		if !strings.HasPrefix(path, "main/") || !strings.Contains(string(data), "package main") {
			t.Fatalf("invalid main route: %s\n%s", path, data)
		}
	}
}
