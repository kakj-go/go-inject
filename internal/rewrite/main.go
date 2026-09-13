package rewrite

import (
	"fmt"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"
)

func validateMainDirectives(rule *template) error {
	var validationErr error
	dst.Inspect(rule.file.file, func(node dst.Node) bool {
		if node == nil || node == rule.file.file || validationErr != nil {
			return validationErr == nil
		}
		value, main := directive(node, "main")
		if !main {
			return true
		}
		fn, ok := node.(*dst.FuncDecl)
		_, add := directive(node, "add")
		if !ok || !add || value != "" || fn.Name.Name != "init" || fn.Recv != nil || fn.Type.TypeParams != nil || len(flatten(fn.Type.Params)) != 0 || len(flatten(fn.Type.Results)) != 0 || fn.Body == nil {
			validationErr = fmt.Errorf("rule %s: declaration //inject:main is allowed only on //inject:add func init() with a body", rule.id)
			return false
		}
		return true
	})
	return validationErr
}

// A declaration routed to main no longer has the target package's lexical
// scope. Require explicit qualified exported references instead of accidentally
// binding a projection or helper to an unrelated identifier in main.
func (e *engine) validateMainBindings(rule *template, node dst.Node) error {
	targetNames := map[string]bool{}
	packageObjects := map[*dst.Object]bool{}
	for name := range e.types {
		targetNames[name] = true
	}
	for name := range e.values {
		targetNames[name] = true
	}
	for name := range e.functions {
		if !strings.Contains(name, ".") {
			targetNames[name] = true
		}
	}
	for _, candidate := range e.rules {
		if candidate.rule.Target == "main" {
			continue
		}
		for _, decl := range candidate.file.file.Decls {
			if _, main := directive(decl, "main"); main {
				continue
			}
			for _, name := range declarationNames(decl) {
				targetNames[name] = true
			}
			switch decl := decl.(type) {
			case *dst.FuncDecl:
				if decl.Name.Obj != nil {
					packageObjects[decl.Name.Obj] = true
				}
			case *dst.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *dst.TypeSpec:
						if spec.Name.Obj != nil {
							packageObjects[spec.Name.Obj] = true
						}
					case *dst.ValueSpec:
						for _, name := range spec.Names {
							if name.Obj != nil {
								packageObjects[name.Obj] = true
							}
						}
					}
				}
			}
		}
	}
	var bindingErr error
	dstutil.Apply(node, func(cursor *dstutil.Cursor) bool {
		if bindingErr != nil {
			return false
		}
		id, ok := cursor.Node().(*dst.Ident)
		if !ok {
			return true
		}
		if fn, ok := cursor.Parent().(*dst.FuncDecl); ok && fn.Name == id {
			return false
		}
		if selector, ok := cursor.Parent().(*dst.SelectorExpr); ok && selector.Sel == id {
			return false
		}
		if _, imported := rule.file.imports[id.Name]; imported && id.Obj == nil {
			return false
		}
		if targetNames[id.Name] && (id.Obj == nil || packageObjects[id.Obj]) {
			bindingErr = fmt.Errorf("rule %s: main initializer cannot reference target-local symbol %s; use an explicitly imported exported symbol", rule.id, id.Name)
			return false
		}
		return true
	}, nil)
	return bindingErr
}
