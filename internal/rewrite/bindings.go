package rewrite

import (
	"fmt"
	"github.com/dave/dst"
)

func checkedPair(rule *template, key, left, right string) (bool, error) {
	if rule.rule.Bindings == nil {
		return false, nil
	}
	proof, ok := rule.rule.Bindings[key]
	if !ok || proof.Template != left || proof.Target != right {
		return false, fmt.Errorf("rule %s: %s changed after semantic binding", rule.id, key)
	}
	return true, nil
}

// BindingShapes captures syntax identities, not type equivalence. Only the
// semantic package may certify these pairs after checking actual Go types.
func BindingShapes(pkg string, sources []Source, rule Rule) (map[string]Binding, error) {
	e := &engine{importPath: pkg, types: map[string]*typeDecl{}, values: map[string]*valueDecl{}, functions: map[string][]*functionDecl{}}
	for _, s := range sources {
		f, err := parse(s.Path, s.Data, s.ImportNames)
		if err != nil {
			return nil, err
		}
		e.sources = append(e.sources, f)
	}
	if err := e.indexTargets(); err != nil {
		return nil, err
	}
	f, err := parse(rule.Path, rule.Source, rule.ImportNames)
	if err != nil {
		return nil, err
	}
	r := &template{rule: rule, file: f, id: rule.ID}
	targetFile, err := e.targetFile(r)
	if err != nil {
		return nil, err
	}
	out := map[string]Binding{}
	for _, d := range f.file.Decls {
		if fn, ok := d.(*dst.FuncDecl); ok {
			if _, add := directive(fn, "add"); add {
				continue
			}
			var found []*functionDecl
			for _, candidate := range e.functions[functionName(fn)] {
				if rule.Target == "main" || rule.File == "" || candidate.file == targetFile {
					found = append(found, candidate)
				}
			}
			if len(found) != 1 {
				return nil, fmt.Errorf("rule %s: function %s matched %d declarations in %s", r.id, functionName(fn), len(found), e.scopeName(r, targetFile))
			}
			left, right, err := e.signaturePair(r, fn, found[0])
			if err != nil {
				return nil, err
			}
			out["func:"+functionName(fn)] = Binding{left, right}
		}
		gen, ok := d.(*dst.GenDecl)
		if !ok {
			continue
		}
		if _, add := directive(gen, "add"); add {
			continue
		}
		for _, sp := range gen.Specs {
			p, ok := sp.(*dst.TypeSpec)
			if !ok {
				continue
			}
			if _, add := directive(p, "add"); add {
				continue
			}
			target := e.types[p.Name.Name]
			if target == nil {
				return nil, fmt.Errorf("rule %s: projected type %s does not exist", r.id, p.Name.Name)
			}
			leftCtx, rightCtx := e.typeContext(f), e.typeContext(target.file)
			leftCtx.bindTypeParameters(p.TypeParams)
			rightCtx.bindTypeParameters(target.spec.TypeParams)
			left, err := leftCtx.fields(p.TypeParams)
			if err != nil {
				return nil, err
			}
			right, err := rightCtx.fields(target.spec.TypeParams)
			if err != nil {
				return nil, err
			}
			out["constraints:"+p.Name.Name] = Binding{left, right}
			ls, lok := p.Type.(*dst.StructType)
			rs, rok := target.spec.Type.(*dst.StructType)
			if !lok || !rok {
				left, err := leftCtx.expr(p.Type)
				if err != nil {
					return nil, err
				}
				right, err := rightCtx.expr(target.spec.Type)
				if err != nil {
					return nil, err
				}
				out["type:"+p.Name.Name] = Binding{left, right}
				continue
			}
			if ls.Fields == nil {
				continue
			}
			for _, lf := range ls.Fields.List {
				if _, add := directive(lf, "add"); add {
					continue
				}
				for _, name := range fieldNames(lf) {
					var actual *dst.Field
					if rs.Fields != nil {
						for _, rf := range rs.Fields.List {
							for _, rn := range fieldNames(rf) {
								if rn == name {
									actual = rf
								}
							}
						}
					}
					if actual == nil {
						return nil, fmt.Errorf("rule %s: required field %s.%s does not exist", r.id, p.Name.Name, name)
					}
					left, err := leftCtx.expr(lf.Type)
					if err != nil {
						return nil, err
					}
					right, err := rightCtx.expr(actual.Type)
					if err != nil {
						return nil, err
					}
					out["field:"+p.Name.Name+"."+name] = Binding{left, right}
				}
			}
		}
	}
	return out, nil
}
