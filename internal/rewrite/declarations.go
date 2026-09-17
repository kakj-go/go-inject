package rewrite

import (
	"crypto/sha256"
	"fmt"
	"go/token"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/dstutil"
)

type typeDecl struct {
	file *parsedFile
	spec *dst.TypeSpec
}
type valueDecl struct {
	file *parsedFile
	spec *dst.ValueSpec
	kind token.Token
}
type functionDecl struct {
	file   *parsedFile
	fn     *dst.FuncDecl
	blocks []injection
	bound  bool
}

func (e *engine) indexTargets() error {
	for _, file := range e.sources {
		for _, decl := range file.file.Decls {
			switch n := decl.(type) {
			case *dst.FuncDecl:
				key := functionName(n)
				e.functions[key] = append(e.functions[key], &functionDecl{file: file, fn: n})
			case *dst.GenDecl:
				for _, spec := range n.Specs {
					switch s := spec.(type) {
					case *dst.TypeSpec:
						if _, exists := e.types[s.Name.Name]; exists {
							return fmt.Errorf("target %s: duplicate type %s", e.importPath, s.Name.Name)
						}
						e.types[s.Name.Name] = &typeDecl{file: file, spec: s}
					case *dst.ValueSpec:
						for _, name := range s.Names {
							if name.Name != "_" {
								e.values[name.Name] = &valueDecl{file: file, spec: s, kind: n.Tok}
							}
						}
					}
				}
			}
		}
	}
	return nil
}

func (e *engine) applyDeclarations(rule *template) error {
	if err := validateMainDirectives(rule); err != nil {
		return err
	}
	target, err := e.targetFile(rule)
	if err != nil {
		return err
	}
	for _, decl := range rule.file.file.Decls {
		if fn, ok := decl.(*dst.FuncDecl); ok {
			if _, add := directive(fn, "add"); add {
				if err := e.addDeclaration(rule, fn); err != nil {
					return err
				}
			}
			continue
		}
		gen, ok := decl.(*dst.GenDecl)
		if !ok || gen.Tok == token.IMPORT {
			continue
		}
		_, allAdd := directive(gen, "add")
		addCount := 0
		for _, spec := range gen.Specs {
			if _, add := directive(spec, "add"); add {
				addCount++
			}
		}
		if allAdd || addCount == len(gen.Specs) && addCount > 0 {
			// A declaration group owns iota's index and implicit const expressions.
			// Moving its specs separately would silently change their values.
			if err := e.addDeclaration(rule, gen); err != nil {
				return err
			}
			continue
		}
		if (gen.Tok == token.CONST || gen.Tok == token.VAR) && addCount > 0 {
			return fmt.Errorf("rule %s: mixed projected and added %s declaration group is unsupported; keep additions in a separate complete declaration group", rule.id, gen.Tok)
		}
		for _, spec := range gen.Specs {
			_, specAdd := directive(spec, "add")
			if allAdd || specAdd {
				copy := clone(gen).(*dst.GenDecl)
				copy.Specs = []dst.Spec{clone(spec).(dst.Spec)}
				if err := e.addDeclaration(rule, copy); err != nil {
					return err
				}
				continue
			}
			switch s := spec.(type) {
			case *dst.TypeSpec:
				if err := e.projectType(rule, s); err != nil {
					return err
				}
			case *dst.ValueSpec:
				if err := e.replaceInitializer(rule, target, namedValue{spec: s, kind: gen.Tok}); err != nil {
					return err
				}
			default:
				return fmt.Errorf("rule %s: unsupported declaration %T", rule.id, spec)
			}
		}
	}
	return nil
}

func (e *engine) additionFile(rule *template, routeMain bool) (*parsedFile, error) {
	digest := sha256.Sum256([]byte(rule.id))
	key := fmt.Sprintf("goinject_%x.go", digest[:8])
	packageName := e.sources[0].file.Name.Name
	if routeMain {
		key = "main/" + key
		packageName = "main"
	}
	for _, file := range e.sources {
		if file.additionKey == key {
			return file, nil
		}
	}
	file, err := parse(key, []byte("package "+packageName+"\n"))
	if err != nil {
		return nil, err
	}
	file.additionKey = key
	file.changed = true
	e.sources = append(e.sources, file)
	return file, nil
}

func declarationNames(decl dst.Decl) []string {
	if fn, ok := decl.(*dst.FuncDecl); ok {
		return []string{functionName(fn)}
	}
	var result []string
	if gen, ok := decl.(*dst.GenDecl); ok {
		for _, spec := range gen.Specs {
			switch n := spec.(type) {
			case *dst.TypeSpec:
				result = append(result, n.Name.Name)
			case *dst.ValueSpec:
				for _, name := range n.Names {
					result = append(result, name.Name)
				}
			}
		}
	}
	return result
}

func (e *engine) addDeclaration(rule *template, decl dst.Decl) error {
	if _, function := decl.(*dst.FuncDecl); !function {
		var invalid bool
		dst.Inspect(decl, func(n dst.Node) bool {
			if n != nil {
				for _, comment := range n.Decorations().Start {
					fields := strings.Fields(comment)
					// Self-linknames (//go:linkname X X) rename a symbol to a
					// bare name so other packages can pull it; they stay
					// valid on value declarations. Anything else on a
					// non-function declaration is an unvalidatable bridge.
					if len(fields) == 3 && fields[0] == "//go:linkname" && fields[1] == fields[2] && !strings.Contains(fields[2], ".") {
						continue
					}
					if strings.HasPrefix(comment, "//go:linkname ") {
						invalid = true
					}
				}
			}
			return true
		})
		if invalid {
			return fmt.Errorf("rule %s: go:linkname bridges require non-generic function declarations; expose shared state through functions", rule.id)
		}
	}
	_, declMain := directive(decl, "main")
	routeMain := rule.rule.Target == "main" || declMain
	file, err := e.additionFile(rule, routeMain)
	if err != nil {
		return err
	}
	for _, name := range declarationNames(decl) {
		if name == "init" || name == "_" {
			continue
		}
		key := name
		if routeMain && e.importPath != "main" {
			key = "main/" + name
		}
		if owner, exists := e.added[key]; exists {
			return fmt.Errorf("rule %s: added declaration %s conflicts with rule %s", rule.id, name, owner)
		}
		if key == name && (e.types[name] != nil || e.values[name] != nil || len(e.functions[name]) > 0) {
			return fmt.Errorf("rule %s: added declaration %s already exists in target %s", rule.id, name, e.importPath)
		}
		e.added[key] = rule.id
	}
	copy := clone(decl).(dst.Decl)
	if fn, ok := copy.(*dst.FuncDecl); ok && fn.Body != nil {
		hygienicLocals(fn.Body, rule.id+":"+fn.Name.Name, nil, e.literalFieldKeys(rule.file, fn.Body))
	}
	if routeMain && e.importPath != "main" && e.sources[0].file.Name.Name != "main" {
		if err := e.validateMainBindings(rule, copy); err != nil {
			return err
		}
	}
	stripDirectives(copy)
	if mapping := sourceMapping(rule.file, decl); mapping != "" {
		copy.Decorations().Start.Prepend(mapping)
	}
	if err := e.rebindImports(rule.file, file, copy); err != nil {
		return err
	}
	if fn, ok := copy.(*dst.FuncDecl); ok && fn.Body == nil {
		if fn.Recv != nil || fn.Type.TypeParams != nil {
			return fmt.Errorf("rule %s: native links require non-generic package-level functions", rule.id)
		}
		var symbol string
		for _, comment := range fn.Decs.Start {
			fields := strings.Fields(comment)
			if len(fields) > 0 && fields[0] == "//go:linkname" {
				if len(fields) != 3 || fields[1] != fn.Name.Name {
					return fmt.Errorf("rule %s: bodyless link requires //go:linkname %s package.symbol", rule.id, fn.Name.Name)
				}
				symbol = fields[2]
			}
		}
		if symbol == "" {
			return fmt.Errorf("rule %s: added bodyless function %s requires a native go:linkname target", rule.id, fn.Name.Name)
		}
		signature, err := e.typeContext(file).function(fn.Type)
		if err != nil {
			return err
		}
		proof, checked := rule.rule.Bindings["bridge:"+fn.Name.Name]
		checked = checked && proof.Target == symbol
		e.result.Links = append(e.result.Links, Link{Symbol: symbol, Signature: signature, Checked: checked})
		if err := e.ensureBlankImport(file, "unsafe"); err != nil {
			return err
		}
	}
	file.file.Decls = append(file.file.Decls, copy)
	for _, name := range declarationNames(decl) {
		e.record(rule, file, "add "+name, 0)
	}
	return nil
}

func (e *engine) projectType(rule *template, projection *dst.TypeSpec) error {
	target, exists := e.types[projection.Name.Name]
	if !exists {
		return fmt.Errorf("rule %s: projected type %s does not exist in target %s", rule.id, projection.Name.Name, e.importPath)
	}
	fromCtx, toCtx := e.typeContext(rule.file), e.typeContext(target.file)
	fromCtx.bindTypeParameters(projection.TypeParams)
	toCtx.bindTypeParameters(target.spec.TypeParams)
	fromConstraints, err := fromCtx.fields(projection.TypeParams)
	if err != nil {
		return err
	}
	toConstraints, err := toCtx.fields(target.spec.TypeParams)
	if err != nil {
		return err
	}
	constraintsChecked, err := checkedPair(rule, "constraints:"+projection.Name.Name, fromConstraints, toConstraints)
	if err != nil {
		return err
	}
	if !e.declarationsOnly && !constraintsChecked && fromConstraints != toConstraints {
		return fmt.Errorf("rule %s: generic type constraints for %s mismatch", rule.id, projection.Name.Name)
	}
	fromStruct, fromOK := projection.Type.(*dst.StructType)
	toStruct, toOK := target.spec.Type.(*dst.StructType)
	if !fromOK || !toOK {
		fromType, err := fromCtx.expr(projection.Type)
		if err != nil {
			return err
		}
		toType, err := toCtx.expr(target.spec.Type)
		if err != nil {
			return err
		}
		checked, err := checkedPair(rule, "type:"+projection.Name.Name, fromType, toType)
		if err != nil {
			return err
		}
		if !e.declarationsOnly && !checked && fromType != toType {
			return fmt.Errorf("rule %s: type projection %s mismatch: %s != %s", rule.id, projection.Name.Name, fromType, toType)
		}
		return nil
	}
	if fromStruct.Fields == nil {
		return nil
	}
	if toStruct.Fields == nil {
		toStruct.Fields = &dst.FieldList{}
	}
	for _, field := range fromStruct.Fields.List {
		_, add := directive(field, "add")
		for _, name := range fieldNames(field) {
			var existing *dst.Field
			for _, candidate := range toStruct.Fields.List {
				for _, candidateName := range fieldNames(candidate) {
					if candidateName == name {
						existing = candidate
					}
				}
			}
			if add && existing != nil {
				return fmt.Errorf("rule %s: added field %s.%s already exists", rule.id, projection.Name.Name, name)
			}
			if !add && existing == nil {
				return fmt.Errorf("rule %s: required field %s.%s does not exist", rule.id, projection.Name.Name, name)
			}
			if !add {
				left, err := fromCtx.expr(field.Type)
				if err != nil {
					return err
				}
				right, err := toCtx.expr(existing.Type)
				if err != nil {
					return err
				}
				checked, err := checkedPair(rule, "field:"+projection.Name.Name+"."+name, left, right)
				if err != nil {
					return err
				}
				if !e.declarationsOnly && !checked && left != right {
					return fmt.Errorf("rule %s: field %s.%s type mismatch: %s != %s", rule.id, projection.Name.Name, name, left, right)
				}
			}
		}
		if add {
			copy := clone(field).(*dst.Field)
			from, to := flatten(projection.TypeParams), flatten(target.spec.TypeParams)
			names := map[string]string{}
			for i, param := range from {
				if len(param.Names) > 0 && len(to[i].Names) > 0 {
					names[param.Names[0].Name] = to[i].Names[0].Name
				}
			}
			dstutil.Apply(copy.Type, func(cursor *dstutil.Cursor) bool {
				if id, ok := cursor.Node().(*dst.Ident); ok {
					if selector, ok := cursor.Parent().(*dst.SelectorExpr); ok && selector.Sel == id {
						return false
					}
					if name, exists := names[id.Name]; exists {
						id.Name = name
					}
				}
				return true
			}, nil)
			stripDirectives(copy)
			if err := e.rebindImports(rule.file, target.file, copy); err != nil {
				return err
			}
			toStruct.Fields.List = append(toStruct.Fields.List, copy)
			e.record(rule, target.file, "field "+projection.Name.Name+"."+strings.Join(fieldNames(field), ","), 0)
		}
	}
	return nil
}

func fieldNames(field *dst.Field) []string {
	var names []string
	for _, name := range field.Names {
		names = append(names, name.Name)
	}
	if len(names) > 0 {
		return names
	}
	var expr dst.Expr = field.Type
	for {
		switch n := expr.(type) {
		case *dst.StarExpr:
			expr = n.X
		case *dst.IndexExpr:
			expr = n.X
		case *dst.IndexListExpr:
			expr = n.X
		case *dst.Ident:
			return []string{n.Name}
		case *dst.SelectorExpr:
			return []string{n.Sel.Name}
		default:
			return []string{"?"}
		}
	}
}

type namedValue struct {
	spec *dst.ValueSpec
	kind token.Token
}

func (e *engine) replaceInitializer(rule *template, _ *parsedFile, source namedValue) error {
	if len(source.spec.Values) == 0 {
		return fmt.Errorf("rule %s: declaration projection requires an initializer or //inject:add", rule.id)
	}
	var target *valueDecl
	for _, name := range source.spec.Names {
		candidate := e.values[name.Name]
		if candidate == nil {
			return fmt.Errorf("rule %s: declaration %s does not exist; new declarations require //inject:add", rule.id, name.Name)
		}
		if candidate.kind != source.kind {
			return fmt.Errorf("rule %s: declaration kind mismatch for %s", rule.id, name.Name)
		}
		if target != nil && target.spec != candidate.spec {
			return fmt.Errorf("rule %s: grouped initializer must match a single target declaration", rule.id)
		}
		target = candidate
	}
	if target == nil {
		return fmt.Errorf("rule %s: initializer has no named target", rule.id)
	}
	if len(target.spec.Names) != len(source.spec.Names) {
		return fmt.Errorf("rule %s: grouped declaration names mismatch", rule.id)
	}
	for i, name := range source.spec.Names {
		if name.Name != target.spec.Names[i].Name {
			return fmt.Errorf("rule %s: declaration position mismatch for %s", rule.id, name.Name)
		}
	}
	key := "initializer:" + strings.Join(fieldIdentifiers(source.spec.Names), ",")
	if owner, exists := e.added[key]; exists {
		return fmt.Errorf("rule %s: initializer conflicts with rule %s", rule.id, owner)
	}
	e.added[key] = rule.id
	copy := clone(source.spec).(*dst.ValueSpec)
	if source.spec.Type != nil && target.spec.Type != nil {
		left, err := e.typeContext(rule.file).expr(source.spec.Type)
		if err != nil {
			return err
		}
		right, err := e.typeContext(target.file).expr(target.spec.Type)
		if err != nil {
			return err
		}
		if left != right {
			return fmt.Errorf("rule %s: initializer type mismatch %s != %s", rule.id, left, right)
		}
	}
	if err := e.rebindImports(rule.file, target.file, copy); err != nil {
		return err
	}
	target.spec.Values = copy.Values
	if target.spec.Type == nil && copy.Type != nil {
		target.spec.Type = copy.Type
	}
	e.record(rule, target.file, key, 0)
	return nil
}

func fieldIdentifiers(ids []*dst.Ident) []string {
	var result []string
	for _, id := range ids {
		result = append(result, id.Name)
	}
	return result
}
