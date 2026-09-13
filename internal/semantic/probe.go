package semantic

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"

	"golang.org/x/tools/go/ast/astutil"
)

type probe struct {
	model   *Model
	target  *types.Package
	mirror  *types.Package
	source  *ast.File
	names   map[string]string
	imports map[string]string
	alias   string
}

func newProbe(m *Model, target *types.Package, file *ast.File) *probe {
	p := &probe{model: m, target: target, source: file, names: map[string]string{}, imports: map[string]string{}, alias: "__goinject_types", mirror: types.NewPackage("goinject.invalid/types", "bindings")}
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		} else if pkg, e := m.Import(path); e == nil {
			name = pkg.Name()
		}
		p.imports[name] = path
	}
	for file.Scope != nil && file.Scope.Lookup(p.alias) != nil || p.imports[p.alias] != "" {
		p.alias += "_"
	}
	for i, name := range target.Scope().Names() {
		obj := target.Scope().Lookup(name)
		public := fmt.Sprintf("Object%d", i)
		var copy types.Object
		switch obj := obj.(type) {
		case *types.TypeName:
			copy = types.NewTypeName(token.NoPos, p.mirror, public, obj.Type())
		case *types.Const:
			copy = types.NewConst(token.NoPos, p.mirror, public, obj.Type(), obj.Val())
		case *types.Var:
			copy = types.NewVar(token.NoPos, p.mirror, public, obj.Type())
		case *types.Func:
			copy = types.NewFunc(token.NoPos, p.mirror, public, obj.Type().(*types.Signature))
		}
		if copy != nil {
			p.mirror.Scope().Insert(copy)
			p.names[name] = public
		}
	}
	p.mirror.MarkComplete()
	return p
}

func (p *probe) Import(path string) (*types.Package, error) {
	if path == p.mirror.Path() {
		return p.mirror, nil
	}
	return p.model.Import(path)
}

func (p *probe) transform(node ast.Node, bound map[string]bool) ast.Node {
	return astutil.Apply(node, func(c *astutil.Cursor) bool {
		if sel, ok := c.Node().(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && p.imports[id.Name] == p.target.Path() {
				if name := p.names[sel.Sel.Name]; name != "" {
					c.Replace(&ast.SelectorExpr{X: ast.NewIdent(p.alias), Sel: ast.NewIdent(name)})
					return false
				}
			}
		}
		id, ok := c.Node().(*ast.Ident)
		if !ok || bound[id.Name] {
			return true
		}
		if f, ok := c.Parent().(*ast.Field); ok {
			for _, name := range f.Names {
				if name == id {
					return false
				}
			}
		}
		if sel, ok := c.Parent().(*ast.SelectorExpr); ok && (sel.Sel == id || p.imports[id.Name] != "") {
			return false
		}
		if types.Universe.Lookup(id.Name) != nil && (p.source.Scope == nil || p.source.Scope.Lookup(id.Name) == nil) {
			return false
		}
		if name := p.names[id.Name]; name != "" {
			c.Replace(&ast.SelectorExpr{X: ast.NewIdent(p.alias), Sel: ast.NewIdent(name)})
			return false
		}
		return true
	}, nil)
}

func bindNames(list *ast.FieldList) map[string]bool {
	out := map[string]bool{}
	if list != nil {
		for _, f := range list.List {
			for _, id := range f.Names {
				out[id.Name] = true
			}
		}
	}
	return out
}

func (p *probe) check(fn *ast.FuncDecl) (*types.Signature, error) {
	fn.Type.Params = unnamedFields(fn.Type.Params)
	fn.Type.Results = unnamedFields(fn.Type.Results)
	bound := bindNames(fn.Type.TypeParams)
	fn.Type = p.transform(fn.Type, bound).(*ast.FuncType)
	fn.Name = ast.NewIdent("__goinject_probe")
	fn.Body = &ast.BlockStmt{}
	fn.Doc = nil
	file := &ast.File{Name: ast.NewIdent(p.target.Name())}
	gen := &ast.GenDecl{Tok: token.IMPORT}
	for _, imp := range p.source.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		if path == p.target.Path() {
			continue
		}
		gen.Specs = append(gen.Specs, &ast.ImportSpec{Name: imp.Name, Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(path)}})
	}
	gen.Specs = append(gen.Specs, &ast.ImportSpec{Name: ast.NewIdent(p.alias), Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(p.mirror.Path())}})
	file.Decls = []ast.Decl{gen, fn}
	var code bytes.Buffer
	if err := format.Node(&code, token.NewFileSet(), file); err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "goinject_signature.go", code.Bytes(), 0)
	if err != nil {
		return nil, err
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	config := types.Config{Importer: p, IgnoreFuncBodies: true, DisableUnusedImportCheck: true}
	pkg, err := config.Check(p.target.Path(), fset, []*ast.File{parsed}, info)
	if err != nil {
		return nil, err
	}
	return pkg.Scope().Lookup(fn.Name.Name).Type().(*types.Signature), nil
}

func unnamedFields(list *ast.FieldList) *ast.FieldList {
	if list == nil {
		return nil
	}
	out := &ast.FieldList{}
	for _, field := range list.List {
		count := len(field.Names)
		if count == 0 {
			count = 1
		}
		for range count {
			out.List = append(out.List, &ast.Field{Type: field.Type})
		}
	}
	return out
}

func (p *probe) constraintExpr(tp *types.TypeParam) ast.Expr {
	// Named target constraints use the mirror and retain their private identity.
	value := types.TypeString(tp.Constraint(), func(pkg *types.Package) string {
		for alias, path := range p.imports {
			if path == pkg.Path() && alias != "_" {
				return alias
			}
		}
		alias := fmt.Sprintf("__constraint%d", len(p.imports))
		p.imports[alias] = pkg.Path()
		p.source.Imports = append(p.source.Imports, &ast.ImportSpec{Name: ast.NewIdent(alias), Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(pkg.Path())}})
		return alias
	})
	expr, err := parser.ParseExpr(value)
	if err != nil {
		return ast.NewIdent("any")
	}
	return expr
}
