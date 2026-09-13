package semantic

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"strings"

	"github.com/kakj-go/go-inject/internal/rewrite"
)

func has(doc *ast.CommentGroup, directive string) bool {
	if doc != nil {
		for _, c := range doc.List {
			if strings.TrimSpace(c.Text) == "//inject:"+directive {
				return true
			}
		}
	}
	return false
}
func text(node ast.Node) string {
	var b bytes.Buffer
	_ = format.Node(&b, token.NewFileSet(), node)
	return b.String()
}
func copyFunc(fn *ast.FuncDecl) *ast.FuncDecl {
	f, _ := parser.ParseFile(token.NewFileSet(), "probe.go", "package probe\n"+text(fn), parser.ParseComments)
	return f.Decls[0].(*ast.FuncDecl)
}
func copyExpr(expr ast.Expr) ast.Expr { out, _ := parser.ParseExpr(text(expr)); return out }

func (m *Model) Bind(path string, sources []rewrite.Source, rules []rewrite.Rule) ([]rewrite.Rule, error) {
	if len(rules) == 0 {
		return rules, nil
	}
	pkg, err := m.Package(path)
	if err != nil {
		return nil, err
	}
	result := append([]rewrite.Rule{}, rules...)
	for i := range result {
		rule := &result[i]
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, rule.Path, rule.Source, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		shapes, err := rewrite.BindingShapes(path, sources, *rule)
		if err != nil {
			return nil, err
		}
		p := newProbe(m, pkg.Types, file)
		fail := func(n ast.Node, e error) error {
			return fmt.Errorf("rule %s (%s): %w", rule.ID, fset.Position(n.Pos()), e)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				bridge := ""
				if fn.Doc != nil {
					for _, c := range fn.Doc.List {
						parts := strings.Fields(c.Text)
						if len(parts) == 3 && parts[0] == "//go:linkname" && parts[1] == fn.Name.Name {
							bridge = parts[2]
						}
					}
				}
				if has(fn.Doc, "add") && bridge == "" {
					continue
				}
				targetPkg := pkg
				name, recv := fn.Name.Name, ""
				if fn.Recv != nil {
					recv = receiverName(text(fn.Recv.List[0].Type))
				}
				if bridge != "" {
					cut := strings.LastIndexByte(bridge, '.')
					if cut < 1 {
						return nil, fail(fn, fmt.Errorf("invalid linkname target %s", bridge))
					}
					targetPkg, err = m.Package(bridge[:cut])
					if err != nil {
						return nil, fail(fn, err)
					}
					name = bridge[cut+1:]
					recv = ""
					body := false
					for _, source := range targetPkg.Syntax {
						for _, d := range source.Decls {
							if f, ok := d.(*ast.FuncDecl); ok && f.Recv == nil && f.Name.Name == name && f.Body != nil {
								body = true
							}
						}
					}
					if !body {
						return nil, fail(fn, fmt.Errorf("bridge target %s has no ordinary Go function body", bridge))
					}
				}
				actual, err := function(targetPkg.Types, name, recv)
				if err != nil {
					return nil, fail(fn, err)
				}
				if bridge != "" && (actual.TypeParams().Len() > 0 || fn.Type.TypeParams != nil) {
					return nil, fail(fn, fmt.Errorf("generic bridges are not supported"))
				}
				probeFn := copyFunc(fn)
				if probeFn.Recv != nil {
					receiver := probeFn.Recv.List[0]
					if probeFn.Type.Params == nil {
						probeFn.Type.Params = &ast.FieldList{}
					}
					receiver.Names = nil
					probeFn.Type.Params.List = append([]*ast.Field{receiver}, probeFn.Type.Params.List...)
					probeFn.Recv = nil
					if actual.RecvTypeParams().Len() > 0 {
						expr := receiver.Type
						if star, ok := expr.(*ast.StarExpr); ok {
							expr = star.X
						}
						var indices []ast.Expr
						switch expr := expr.(type) {
						case *ast.IndexExpr:
							indices = []ast.Expr{expr.Index}
						case *ast.IndexListExpr:
							indices = expr.Indices
						}
						if len(indices) != actual.RecvTypeParams().Len() {
							return nil, fail(fn, fmt.Errorf("receiver type parameter count mismatch"))
						}
						probeFn.Type.TypeParams = &ast.FieldList{}
						for j, index := range indices {
							id, ok := index.(*ast.Ident)
							if !ok {
								return nil, fail(fn, fmt.Errorf("invalid receiver type parameter"))
							}
							probeFn.Type.TypeParams.List = append(probeFn.Type.TypeParams.List, &ast.Field{Names: []*ast.Ident{id}, Type: p.constraintExpr(actual.RecvTypeParams().At(j))})
						}
					}
				}
				expected, err := p.check(probeFn)
				if err != nil {
					return nil, fail(fn, err)
				}
				if !equivalent(expected, withReceiver(actual)) {
					return nil, fail(fn, fmt.Errorf("signature mismatch for %s: template %s; target %s", name, expected, withReceiver(actual)))
				}
				if bridge != "" {
					shapes["bridge:"+fn.Name.Name] = rewrite.Binding{Target: bridge}
				}
			}
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE || has(gen.Doc, "add") {
				continue
			}
			for _, spec := range gen.Specs {
				typ := spec.(*ast.TypeSpec)
				if has(typ.Doc, "add") {
					continue
				}
				obj, ok := pkg.Types.Scope().Lookup(typ.Name.Name).(*types.TypeName)
				if !ok {
					return nil, fail(typ, fmt.Errorf("projected type %s does not exist", typ.Name.Name))
				}
				actual := types.Unalias(obj.Type())
				var tps []*types.TypeParam
				if alias, ok := obj.Type().(*types.Alias); ok {
					tps = params(alias.TypeParams())
				}
				if named, ok := actual.(*types.Named); ok {
					tps = params(named.TypeParams())
				}
				fn := &ast.FuncDecl{Name: ast.NewIdent("probe"), Type: &ast.FuncType{TypeParams: typ.TypeParams, Params: &ast.FieldList{}}}
				checked, err := p.check(copyFunc(fn))
				if err != nil {
					return nil, fail(typ, err)
				}
				want := signature(tps, types.NewTuple(), types.NewTuple(), false)
				if !equivalent(checked, want) {
					return nil, fail(typ, fmt.Errorf("generic constraints mismatch for %s", typ.Name.Name))
				}
				projected, isStruct := typ.Type.(*ast.StructType)
				targetStruct, targetIsStruct := actual.Underlying().(*types.Struct)
				if isStruct && targetIsStruct {
					for _, field := range projected.Fields.List {
						if has(field.Doc, "add") {
							continue
						}
						names := []string{}
						for _, n := range field.Names {
							names = append(names, n.Name)
						}
						if len(names) == 0 {
							expr := field.Type
							if ptr, ok := expr.(*ast.StarExpr); ok {
								expr = ptr.X
							}
							switch expr := expr.(type) {
							case *ast.Ident:
								names = []string{expr.Name}
							case *ast.SelectorExpr:
								names = []string{expr.Sel.Name}
							}
						}
						for _, name := range names {
							var real *types.Var
							for j := 0; j < targetStruct.NumFields(); j++ {
								if targetStruct.Field(j).Name() == name {
									real = targetStruct.Field(j)
								}
							}
							if real == nil {
								return nil, fail(field, fmt.Errorf("required field %s.%s does not exist", typ.Name.Name, name))
							}
							fn := &ast.FuncDecl{Name: ast.NewIdent("probe"), Type: &ast.FuncType{TypeParams: typ.TypeParams, Params: &ast.FieldList{List: []*ast.Field{{Type: copyExpr(field.Type)}}}}}
							sig, err := p.check(copyFunc(fn))
							if err != nil {
								return nil, fail(field, err)
							}
							want := signature(tps, types.NewTuple(types.NewVar(token.NoPos, nil, "", real.Type())), types.NewTuple(), false)
							if !equivalent(sig, want) {
								return nil, fail(field, fmt.Errorf("field %s.%s type mismatch: %s != %s", typ.Name.Name, name, sig, want))
							}
						}
					}
				} else {
					fn := &ast.FuncDecl{Name: ast.NewIdent("probe"), Type: &ast.FuncType{TypeParams: typ.TypeParams, Params: &ast.FieldList{List: []*ast.Field{{Type: copyExpr(typ.Type)}}}}}
					sig, err := p.check(copyFunc(fn))
					if err != nil {
						return nil, fail(typ, err)
					}
					typValue := sig.Params().At(0).Type().Underlying()
					left := signature(params(sig.TypeParams()), types.NewTuple(types.NewVar(token.NoPos, nil, "", typValue)), types.NewTuple(), false)
					right := signature(tps, types.NewTuple(types.NewVar(token.NoPos, nil, "", actual.Underlying())), types.NewTuple(), false)
					if !equivalent(left, right) {
						return nil, fail(typ, fmt.Errorf("type projection %s mismatch", typ.Name.Name))
					}
				}
			}
		}
		rule.Bindings = shapes
	}
	return result, nil
}
