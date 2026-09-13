package semantic

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

func checkedPackage(t *testing.T, source string) *types.Package {
	t.Helper()
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "model.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := new(types.Config).Check("example.test/model", set, []*ast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestEquivalentAcrossIndependentGoLoads(t *testing.T) {
	source := `package model
type Item[T any] struct{ value T }
type List[T any] = []Item[T]
func F[T interface{~int|~int64}](v List[T], f func(T)(T,error)) map[string]T{return nil}
`
	a, b := checkedPackage(t, source), checkedPackage(t, source)
	left, right := a.Scope().Lookup("F").Type(), b.Scope().Lookup("F").Type()
	if types.Identical(left, right) {
		t.Fatal("fixture must contain independently loaded named origins")
	}
	if !equivalent(left, right) {
		t.Fatalf("equivalent typed graphs were rejected: %s / %s", left, right)
	}
}

func TestDistinctGoTypesAndConstraintsRemainDistinct(t *testing.T) {
	sources := []string{
		"package model; func F(v []int){}",
		"package model; func F(v []string){}",
		"package model; func F[T ~int](v T){}",
		"package model; func F[T ~string](v T){}",
		"package model; func F(v [2]int){}",
		"package model; func F(v [3]int){}",
	}
	for i := 0; i < len(sources); i += 2 {
		a, b := checkedPackage(t, sources[i]), checkedPackage(t, sources[i+1])
		if equivalent(a.Scope().Lookup("F").Type(), b.Scope().Lookup("F").Type()) {
			t.Fatalf("different types accepted: %s / %s", sources[i], sources[i+1])
		}
	}
}
