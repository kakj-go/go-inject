package rewrite

import (
	"crypto/sha256"
	"fmt"
	"go/token"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"
)

// rebindImports maintains file-scoped package bindings when a template is moved.
// Always using a fresh destination alias avoids capture by a template local.
func (e *engine) rebindImports(from, to *parsedFile, node dst.Node) error {
	destination := e.importPath
	if strings.HasPrefix(to.additionKey, "main/") {
		destination = "main"
	}
	needed := map[string]string{}
	var bindingErr error
	if _, exists := from.imports["."]; exists {
		return fmt.Errorf("template %s: dot imports require explicit aliases", from.path)
	}
	dstutil.Apply(node, func(cursor *dstutil.Cursor) bool {
		selector, ok := cursor.Node().(*dst.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := selector.X.(*dst.Ident)
		if !ok || ident.Obj != nil {
			return true
		}
		importPath, exists := from.imports[ident.Name]
		if !exists {
			return true
		}
		if destination == "main" && importPath == e.importPath && e.importPath != "main" && !token.IsExported(selector.Sel.Name) {
			bindingErr = fmt.Errorf("main initializer cannot access unexported target symbol %s.%s", importPath, selector.Sel.Name)
			return false
		}
		if importPath == destination {
			replacement := dst.Clone(selector.Sel).(*dst.Ident)
			replacement.Decs.NodeDecs = selector.Decs.NodeDecs
			cursor.Replace(replacement)
			return false
		}
		alias, exists := needed[importPath]
		if !exists {
			var err error
			alias, err = e.ensureImport(to, importPath)
			if err != nil {
				bindingErr = err
				return false
			}
			needed[importPath] = alias
		}
		ident.Name = alias
		return true
	}, nil)
	if bindingErr != nil {
		return bindingErr
	}
	// Blank imports have intentional initialization effects. unsafe also enables
	// go:linkname and must survive even when no selector refers to it.
	if importPath, exists := from.imports["_"]; exists {
		if importPath != destination {
			if err := e.ensureBlankImport(to, importPath); err != nil {
				return err
			}
		}
	}
	for _, imp := range from.file.Imports {
		if imp.Name != nil && imp.Name.Name == "_" {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p != destination {
				if err := e.ensureBlankImport(to, p); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (e *engine) ensureImport(file *parsedFile, importPath string) (string, error) {
	for alias, p := range file.imports {
		if p == importPath && strings.HasPrefix(alias, "__goinject_pkg_") {
			return alias, nil
		}
	}
	digest := sha256.Sum256([]byte(importPath))
	base := fmt.Sprintf("__goinject_pkg_%x", digest[:5])
	alias := base
	used := map[string]bool{}
	dst.Inspect(file.file, func(n dst.Node) bool {
		if id, ok := n.(*dst.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	for i := 1; used[alias]; i++ {
		alias = fmt.Sprintf("%s_%d", base, i)
	}
	for old, p := range file.imports {
		if p != importPath {
			continue
		}
		if old == "." {
			return "", fmt.Errorf("target %s: cannot add named use of dot-imported %s", file.path, p)
		}
		if old != "_" {
			dst.Inspect(file.file, func(n dst.Node) bool {
				if sel, ok := n.(*dst.SelectorExpr); ok {
					if id, ok := sel.X.(*dst.Ident); ok && id.Obj == nil && id.Name == old {
						id.Name = alias
					}
				}
				return true
			})
		}
		for _, imp := range file.file.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p == importPath {
				imp.Name = dst.NewIdent(alias)
			}
		}
		delete(file.imports, old)
		file.imports[alias] = importPath
		return alias, nil
	}
	appendImport(file, alias, importPath)
	e.imports[importPath] = true
	return alias, nil
}

func (e *engine) ensureBlankImport(file *parsedFile, importPath string) error {
	for _, imp := range file.file.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if p == importPath {
			return nil
		}
	}
	appendImport(file, "_", importPath)
	e.imports[importPath] = true
	return nil
}

func pruneImports(file *parsedFile) {
	used := map[string]bool{}
	dst.Inspect(file.file, func(n dst.Node) bool {
		if selector, ok := n.(*dst.SelectorExpr); ok {
			if id, ok := selector.X.(*dst.Ident); ok && id.Obj == nil {
				used[id.Name] = true
			}
		}
		return true
	})
	var declarations []dst.Decl
	var imports []*dst.ImportSpec
	for _, decl := range file.file.Decls {
		gen, ok := decl.(*dst.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			declarations = append(declarations, decl)
			continue
		}
		var specs []dst.Spec
		for _, spec := range gen.Specs {
			imp := spec.(*dst.ImportSpec)
			p, _ := strconv.Unquote(imp.Path.Value)
			alias := importName(p)
			for name, importPath := range file.imports {
				if importPath == p {
					alias = name
					break
				}
			}
			if imp.Name != nil {
				alias = imp.Name.Name
			}
			if alias == "_" || alias == "." || used[alias] {
				specs = append(specs, imp)
				imports = append(imports, imp)
			}
		}
		if len(specs) > 0 {
			gen.Specs = specs
			declarations = append(declarations, gen)
		}
	}
	file.file.Decls = declarations
	file.file.Imports = imports
}

func appendImport(file *parsedFile, alias, importPath string) {
	imp := &dst.ImportSpec{Name: dst.NewIdent(alias), Path: &dst.BasicLit{Kind: token.STRING, Value: strconv.Quote(importPath)}}
	var declaration *dst.GenDecl
	for _, decl := range file.file.Decls {
		if gen, ok := decl.(*dst.GenDecl); ok && gen.Tok == token.IMPORT {
			declaration = gen
			break
		}
	}
	if declaration == nil {
		declaration = &dst.GenDecl{Tok: token.IMPORT}
		file.file.Decls = append([]dst.Decl{declaration}, file.file.Decls...)
	}
	declaration.Specs = append(declaration.Specs, imp)
	file.file.Imports = append(file.file.Imports, imp)
	file.imports[alias] = importPath
}
