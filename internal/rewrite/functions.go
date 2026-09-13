package rewrite

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"
)

type injection struct {
	rule  string
	order int
	body  *dst.BlockStmt
}

func (e *engine) applyFunctions(rule *template) error {
	targetFile, err := e.targetFile(rule)
	if err != nil {
		return err
	}
	for _, decl := range rule.file.file.Decls {
		fn, ok := decl.(*dst.FuncDecl)
		if !ok {
			continue
		}
		if _, add := directive(fn, "add"); add {
			continue
		}
		if fn.Body == nil {
			return fmt.Errorf("rule %s: bodyless function %s requires //inject:add and go:linkname", rule.id, fn.Name.Name)
		}
		key := functionName(fn)
		var candidates []*functionDecl
		for _, candidate := range e.functions[key] {
			if candidate.file == targetFile || rule.rule.Target == "main" {
				candidates = append(candidates, candidate)
			}
		}
		if len(candidates) != 1 {
			return fmt.Errorf("rule %s: function %s matched %d declarations in %s", rule.id, key, len(candidates), targetFile.path)
		}
		target := candidates[0]
		if target.fn.Body == nil {
			return fmt.Errorf("rule %s: target %s has no Go body", rule.id, key)
		}
		if err := e.checkSignature(rule, fn, target); err != nil {
			return err
		}
		n, err := order(fn)
		if err != nil {
			return fmt.Errorf("rule %s: %w", rule.id, err)
		}
		if !target.bound {
			bindTarget(target)
			target.bound = true
		}
		copy := clone(fn).(*dst.FuncDecl)
		if err := bindTemplate(copy, target.fn, rule.id); err != nil {
			return fmt.Errorf("rule %s: %w", rule.id, err)
		}
		body := copy.Body
		if last := len(body.List) - 1; last >= 0 {
			if _, placeholder := body.List[last].(*dst.ReturnStmt); placeholder {
				body.List = body.List[:last]
			}
		}
		stripDirectives(body)
		if err := e.rebindImports(rule.file, target.file, body); err != nil {
			return err
		}
		if mapping := sourceMapping(rule.file, fn); mapping != "" {
			body.Decs.Start.Prepend(mapping)
		}
		body.Decs.Before = dst.NewLine
		body.Decs.After = dst.NewLine
		target.blocks = append(target.blocks, injection{rule: rule.id, order: n, body: body})
		e.record(rule, target.file, key, n)
	}
	return nil
}

func receiverParameters(fn *dst.FuncDecl) []*dst.Ident {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return nil
	}
	var expr dst.Expr = fn.Recv.List[0].Type
	if star, ok := expr.(*dst.StarExpr); ok {
		expr = star.X
	}
	var result []*dst.Ident
	switch n := expr.(type) {
	case *dst.IndexExpr:
		if id, ok := n.Index.(*dst.Ident); ok {
			result = append(result, id)
		}
	case *dst.IndexListExpr:
		for _, index := range n.Indices {
			if id, ok := index.(*dst.Ident); ok {
				result = append(result, id)
			}
		}
	}
	return result
}

func bindReceiverTypes(ctx *typeContext, fn *dst.FuncDecl) {
	for i, id := range receiverParameters(fn) {
		ctx.params[id.Name] = fmt.Sprintf("$R%d", i)
	}
}

func (e *engine) checkSignature(rule *template, fn *dst.FuncDecl, target *functionDecl) error {
	left, right, err := e.signaturePair(rule, fn, target)
	if err != nil {
		return err
	}
	if rule.rule.Bindings != nil {
		proof, ok := rule.rule.Bindings["func:"+functionName(fn)]
		if !ok || proof.Template != left || proof.Target != right {
			return fmt.Errorf("rule %s: signature changed after semantic binding for %s", rule.id, functionName(fn))
		}
		return nil
	}
	if left != right {
		return fmt.Errorf("rule %s: signature mismatch for %s: template %s; target %s", rule.id, functionName(fn), left, right)
	}
	return nil
}

func (e *engine) signaturePair(rule *template, fn *dst.FuncDecl, target *functionDecl) (string, string, error) {
	leftCtx, rightCtx := e.typeContext(rule.file), e.typeContext(target.file)
	bindReceiverTypes(leftCtx, fn)
	bindReceiverTypes(rightCtx, target.fn)
	left, err := leftCtx.function(fn.Type)
	if err != nil {
		return "", "", fmt.Errorf("rule %s signature: %w", rule.id, err)
	}
	right, err := rightCtx.function(target.fn.Type)
	if err != nil {
		return "", "", fmt.Errorf("target %s signature: %w", target.file.path, err)
	}
	leftRecv, err := leftCtx.fields(fn.Recv)
	if err != nil {
		return "", "", err
	}
	rightRecv, err := rightCtx.fields(target.fn.Recv)
	if err != nil {
		return "", "", err
	}
	return leftRecv + " " + left, rightRecv + " " + right, nil
}

func bindTarget(target *functionDecl) {
	used := map[string]bool{}
	dst.Inspect(target.fn, func(n dst.Node) bool {
		if id, ok := n.(*dst.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	bindings := map[*dst.Object]string{}
	for group, fields := range []*dst.FieldList{target.fn.Recv, target.fn.Type.Params, target.fn.Type.Results} {
		if fields == nil {
			continue
		}
		index := 0
		for _, field := range fields.List {
			if len(field.Names) == 0 {
				field.Names = []*dst.Ident{dst.NewIdent("")}
			}
			for _, name := range field.Names {
				base := fmt.Sprintf("__goinject_%d_%d", group, index)
				fresh := base
				for suffix := 1; used[fresh]; suffix++ {
					fresh = fmt.Sprintf("%s_%d", base, suffix)
				}
				used[fresh] = true
				if name.Obj != nil {
					bindings[name.Obj] = fresh
				}
				name.Name = fresh
				index++
			}
		}
	}
	dst.Inspect(target.fn, func(n dst.Node) bool {
		if id, ok := n.(*dst.Ident); ok {
			if name, exists := bindings[id.Obj]; exists {
				id.Name = name
			}
		}
		return true
	})
}

func bindTemplate(fn, target *dst.FuncDecl, rule string) error {
	bindings := map[*dst.Object]string{}
	generic := map[string]string{}
	groups := []struct{ from, to *dst.FieldList }{{fn.Recv, target.Recv}, {fn.Type.Params, target.Type.Params}, {fn.Type.Results, target.Type.Results}, {fn.Type.TypeParams, target.Type.TypeParams}}
	for group, pair := range groups {
		from, to := flatten(pair.from), flatten(pair.to)
		if len(from) != len(to) {
			return fmt.Errorf("parameter group %d mismatch", group)
		}
		for i, field := range from {
			if len(field.Names) == 0 || field.Names[0].Name == "_" {
				continue
			}
			if len(to[i].Names) == 0 {
				return fmt.Errorf("target parameter %d is not bound", i)
			}
			if field.Names[0].Obj != nil {
				bindings[field.Names[0].Obj] = to[i].Names[0].Name
			}
			if group == 3 {
				generic[field.Names[0].Name] = to[i].Names[0].Name
			}
		}
	}
	fromReceiver, toReceiver := receiverParameters(fn), receiverParameters(target)
	if len(fromReceiver) != len(toReceiver) {
		return fmt.Errorf("generic receiver mismatch")
	}
	for i, id := range fromReceiver {
		if id.Obj != nil {
			bindings[id.Obj] = toReceiver[i].Name
		}
		generic[id.Name] = toReceiver[i].Name
	}
	dstutil.Apply(fn.Body, func(cursor *dstutil.Cursor) bool {
		if id, ok := cursor.Node().(*dst.Ident); ok {
			if selector, ok := cursor.Parent().(*dst.SelectorExpr); ok && selector.Sel == id {
				return false
			}
			if name, exists := bindings[id.Obj]; exists {
				id.Name = name
			} else if id.Obj == nil {
				if name, exists := generic[id.Name]; exists {
					id.Name = name
				}
			}
		}
		return true
	}, nil)
	hygienicLocals(fn.Body, rule, bindings)
	// Labels have function scope rather than block scope. Prefix each template's
	// labels so two otherwise isolated template blocks cannot conflict.
	digest := sha256.Sum256([]byte(rule))
	labels := map[string]string{}
	dst.Inspect(fn.Body, func(n dst.Node) bool {
		if label, ok := n.(*dst.LabeledStmt); ok {
			labels[label.Label.Name] = fmt.Sprintf("__goinject_label_%x_%s", digest[:5], label.Label.Name)
		}
		return true
	})
	dst.Inspect(fn.Body, func(n dst.Node) bool {
		switch node := n.(type) {
		case *dst.LabeledStmt:
			node.Label.Name = labels[node.Label.Name]
		case *dst.BranchStmt:
			if node.Label != nil {
				if name, exists := labels[node.Label.Name]; exists {
					node.Label.Name = name
				}
			}
		}
		return true
	})
	return nil
}

func hygienicLocals(body dst.Node, identity string, excluded map[*dst.Object]string) {
	digest := sha256.Sum256([]byte(identity))
	locals := map[*dst.Object]string{}
	used := map[string]bool{}
	dst.Inspect(body, func(n dst.Node) bool {
		if id, ok := n.(*dst.Ident); ok {
			used[id.Name] = true
		}
		return true
	})
	add := func(id *dst.Ident) {
		if id == nil || id.Obj == nil || id.Name == "_" {
			return
		}
		if _, bound := excluded[id.Obj]; bound {
			return
		}
		if _, exists := locals[id.Obj]; !exists {
			base := fmt.Sprintf("__goinject_local_%x_%d", digest[:5], len(locals))
			fresh := base
			for suffix := 1; used[fresh]; suffix++ {
				fresh = fmt.Sprintf("%s_%d", base, suffix)
			}
			locals[id.Obj] = fresh
			used[fresh] = true
		}
	}
	addFields := func(fields *dst.FieldList) {
		if fields != nil {
			for _, field := range fields.List {
				for _, name := range field.Names {
					add(name)
				}
			}
		}
	}
	dst.Inspect(body, func(n dst.Node) bool {
		switch node := n.(type) {
		case *dst.AssignStmt:
			if node.Tok.String() == ":=" {
				for _, expr := range node.Lhs {
					if id, ok := expr.(*dst.Ident); ok {
						add(id)
					}
				}
			}
		case *dst.RangeStmt:
			if node.Tok.String() == ":=" {
				if id, ok := node.Key.(*dst.Ident); ok {
					add(id)
				}
				if id, ok := node.Value.(*dst.Ident); ok {
					add(id)
				}
			}
		case *dst.ValueSpec:
			for _, name := range node.Names {
				add(name)
			}
		case *dst.TypeSpec:
			add(node.Name)
		case *dst.FuncLit:
			addFields(node.Type.Params)
			addFields(node.Type.Results)
			addFields(node.Type.TypeParams)
		}
		return true
	})
	dst.Inspect(body, func(n dst.Node) bool {
		if id, ok := n.(*dst.Ident); ok {
			if name, exists := locals[id.Obj]; exists {
				id.Name = name
			}
		}
		return true
	})
}

func (e *engine) finishFunctions() error {
	for _, functions := range e.functions {
		for _, target := range functions {
			if len(target.blocks) == 0 {
				continue
			}
			sort.Slice(target.blocks, func(i, j int) bool {
				a, b := target.blocks[i], target.blocks[j]
				if a.order != b.order {
					return a.order < b.order
				}
				return a.rule < b.rule
			})
			original := target.fn.Body.List
			var statements []dst.Stmt
			for _, block := range target.blocks {
				statements = append(statements, block.body)
			}
			target.fn.Body.List = append(statements, original...)
		}
	}
	return nil
}

// CanonicalSignature resolves a native bridge target using the same normalized
// signature representation returned in Result.Links. Methods, generics, bodyless
// functions and compiler intrinsics are deliberately outside the bridge contract.
func CanonicalSignature(importPath string, sources []Source, symbol string) (string, error) {
	if strings.ContainsAny(symbol, ".()[]/") {
		return "", fmt.Errorf("bridge target must be a package-level function name: %s", symbol)
	}
	e := &engine{importPath: importPath, types: map[string]*typeDecl{}, values: map[string]*valueDecl{}, functions: map[string][]*functionDecl{}}
	for _, source := range sources {
		file, err := parse(source.Path, source.Data, source.ImportNames)
		if err != nil {
			return "", err
		}
		e.sources = append(e.sources, file)
	}
	if err := e.indexTargets(); err != nil {
		return "", err
	}
	functions := e.functions[symbol]
	if len(functions) != 1 {
		return "", fmt.Errorf("bridge target %s.%s matched %d functions", importPath, symbol, len(functions))
	}
	fn := functions[0]
	if fn.fn.Recv != nil || fn.fn.Type.TypeParams != nil || fn.fn.Body == nil {
		return "", fmt.Errorf("bridge target %s.%s must be a non-generic Go function with a body", importPath, symbol)
	}
	return e.typeContext(fn.file).function(fn.fn.Type)
}
