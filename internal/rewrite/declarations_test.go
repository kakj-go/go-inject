package rewrite

import (
	"strings"
	"testing"
)

func TestAddedConstAndVarGroupsRetainIotaAndInheritedExpressions(t *testing.T) {
	sources := []Source{source("main.go", `package main
import "fmt"
func main(){fmt.Println(First,Second,Third,MaskA,MaskB,Sum,Product)}
`)}
	rules := []Rule{rule("groups", "example.test/target/main.go", `package main
//inject:add
const (
 First=iota+10
 Second
 Third
)
const (
 //inject:add
 MaskA=1<<iota
 //inject:add
 MaskB
)
//inject:add
var (
 Sum=First+Second
 Product=Sum*MaskB
)
`)}
	result, err := Package("example.test/target", sources, rules)
	if err != nil {
		t.Fatal(err)
	}
	execute(t, sources, result, "10 11 12 1 2 21 42")
}

func TestMixedAddedAndProjectedValueGroupsAreRejected(t *testing.T) {
	for _, kind := range []string{"const", "var"} {
		t.Run(kind, func(t *testing.T) {
			sources := []Source{source("target.go", "package target\n"+kind+" Old=1\n")}
			rules := []Rule{rule("mixed", "example.test/target/target.go", "package target\n"+kind+" (\nOld=2\n//inject:add\nNew=3\n)\n")}
			if _, err := Package("example.test/target", sources, rules); err == nil || !strings.Contains(err.Error(), "mixed projected and added") {
				t.Fatalf("want mixed-group rejection, got %v", err)
			}
		})
	}
}

func TestDeclarationMainMarkerRoutesOnlyThatInitializer(t *testing.T) {
	sources := []Source{source("target.go", "package target\nfunc Existing(){}\n")}
	rules := []Rule{rule("route", "example.test/target/target.go", `package hooks
//inject:add
func init(){println("target-init")}
//inject:add
//inject:main
func init(){println("main-init")}
`)}
	result, err := Package("example.test/target", sources, rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Additions) != 2 {
		t.Fatalf("want separate target/main files, got %#v", result.Additions)
	}
	for path, data := range result.Additions {
		text := string(data)
		if strings.HasPrefix(path, "main/") {
			if !strings.Contains(text, "package main") || !strings.Contains(text, "main-init") || strings.Contains(text, "target-init") {
				t.Fatalf("invalid main addition:\n%s", text)
			}
		} else if !strings.Contains(text, "package target") || !strings.Contains(text, "target-init") || strings.Contains(text, "main-init") {
			t.Fatalf("invalid target addition:\n%s", text)
		}
	}
}

func TestDeclarationMainMarkerRejectsOtherDeclarations(t *testing.T) {
	for _, decl := range []string{
		"//inject:main\nfunc init(){}",
		"//inject:add\n//inject:main\nfunc Helper(){}",
		"//inject:add\n//inject:main\nfunc init(x int){}",
		"//inject:add\n//inject:main\nvar Value int",
		"//inject:add\n//inject:main\ntype Added struct{}",
	} {
		_, err := Package("example.test/target", []Source{source("target.go", "package target\nfunc init(){}")}, []Rule{rule("bad-main", "example.test/target/target.go", "package hooks\n"+decl)})
		if err == nil || !strings.Contains(err.Error(), "allowed only on") {
			t.Fatalf("%s: want main marker rejection, got %v", decl, err)
		}
	}
}

func TestRoutedMainKeepsTargetImportQualified(t *testing.T) {
	sources := []Source{source("target.go", "package target\ntype Public struct{}\nfunc Initialize(Public){}\n")}
	rules := []Rule{rule("main-self", "example.test/target/target.go", `package hooks
import target "example.test/target"
//inject:add
//inject:main
func init(){target.Initialize(target.Public{})}
`)}
	result, err := Package("example.test/target", sources, rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Imports) != 1 || result.Imports[0] != "example.test/target" {
		t.Fatalf("lost destination-main dependency: %#v", result.Imports)
	}
	for path, data := range result.Additions {
		if !strings.HasPrefix(path, "main/") || !strings.Contains(string(data), `.Initialize(`) || !strings.Contains(string(data), `.Public{`) {
			t.Fatalf("lost qualified reference: %s\n%s", path, data)
		}
	}
}

func TestRoutedMainRejectsImplicitTargetTypesAndPrivateSelectors(t *testing.T) {
	sources := []Source{source("target.go", "package target\ntype Public struct{}\ntype private struct{}\n")}
	for _, body := range []string{
		"type Public struct{}\n//inject:add\n//inject:main\nfunc init(){_=Public{}}",
		"import target \"example.test/target\"\n//inject:add\n//inject:main\nfunc init(){_=target.private{}}",
	} {
		_, err := Package("example.test/target", sources, []Rule{rule("bad-binding", "example.test/target/target.go", "package hooks\n"+body)})
		if err == nil || !strings.Contains(err.Error(), "main initializer cannot") {
			t.Fatalf("want destination scope rejection, got %v", err)
		}
	}
}
