// Package semantic binds templates using the real Go package/type system.
package semantic

import (
	"context"
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"github.com/kakj-go/go-inject/internal/project"
	"golang.org/x/tools/go/packages"
)

type Model struct {
	ctx         context.Context
	dir         string
	flags       []string
	overlay     map[string][]byte
	root        string
	tests       bool
	entries     []string
	loaded      map[string]*packages.Package
	imports     map[string]*types.Package
	Environment []string
}

func (m *Model) SetFlags(flags []string) { m.flags = flags }

func New(ctx context.Context, dir string, flags []string, overlay map[string][]byte, root string, tests bool, entries ...string) *Model {
	return &Model{ctx: ctx, dir: dir, flags: project.ListFlags(flags), overlay: overlay, root: root, tests: tests, entries: entries, loaded: map[string]*packages.Package{}, imports: map[string]*types.Package{}}
}

func (m *Model) Package(path string) (*packages.Package, error) {
	if p := m.loaded[path]; p != nil {
		return p, nil
	}
	query := path
	if path == m.root+".test" {
		query = m.root
	}
	cmd := project.Command(m.ctx, m.dir)
	if m.Environment != nil {
		cmd.Env = m.Environment
	}
	patterns := []string{query}
	if query == m.root && len(m.entries) > 0 {
		patterns = m.entries
	}
	list, err := packages.Load(&packages.Config{Context: m.ctx, Dir: cmd.Dir, Env: cmd.Environ(), BuildFlags: m.flags, Overlay: m.overlay, Tests: m.tests && query == m.root,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedTypes | packages.NeedTypesSizes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedImports | packages.NeedForTest}, patterns...)
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		for _, e := range p.Errors {
			return nil, fmt.Errorf("type-check %s: %s", p.PkgPath, e)
		}
		if p.Types == nil {
			continue
		}
		old := m.loaded[p.PkgPath]
		if old == nil || p.ForTest == m.root {
			m.loaded[p.PkgPath] = p
		}
		m.collect(p.Types)
	}
	p := m.loaded[path]
	if p == nil {
		return nil, fmt.Errorf("cannot load Go types for %s", path)
	}
	return p, nil
}

func (m *Model) collect(p *types.Package) {
	if m.imports[p.Path()] != nil {
		return
	}
	m.imports[p.Path()] = p
	for _, q := range p.Imports() {
		m.collect(q)
	}
}

func (m *Model) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if p := m.imports[path]; p != nil {
		return p, nil
	}
	p, e := m.Package(path)
	if e != nil {
		return nil, e
	}
	return p.Types, nil
}

func function(p *types.Package, name, receiver string) (*types.Signature, error) {
	if receiver == "" {
		o, ok := p.Scope().Lookup(name).(*types.Func)
		if !ok {
			return nil, fmt.Errorf("function %s does not exist in %s", name, p.Path())
		}
		return o.Type().(*types.Signature), nil
	}
	obj, ok := p.Scope().Lookup(receiver).(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("receiver %s does not exist", receiver)
	}
	named, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok {
		return nil, fmt.Errorf("receiver %s is not a named Go type", receiver)
	}
	for i := 0; i < named.NumMethods(); i++ {
		method := named.Method(i)
		if method.Name() == name {
			return method.Type().(*types.Signature), nil
		}
	}
	return nil, fmt.Errorf("method %s.%s does not exist", receiver, name)
}

func params(list *types.TypeParamList) []*types.TypeParam {
	var out []*types.TypeParam
	for i := 0; i < list.Len(); i++ {
		out = append(out, list.At(i))
	}
	return out
}

// Receiver type parameters become ordinary function type parameters, allowing
// types.Identical to compare alpha-equivalent generic methods as well as funcs.
func withReceiver(sig *types.Signature) *types.Signature {
	if sig.Recv() == nil {
		return sig
	}
	vars := []*types.Var{types.NewVar(token.NoPos, nil, "", sig.Recv().Type())}
	for i := 0; i < sig.Params().Len(); i++ {
		vars = append(vars, sig.Params().At(i))
	}
	return signature(params(sig.RecvTypeParams()), types.NewTuple(vars...), sig.Results(), sig.Variadic())
}

func receiverName(text string) string {
	text = strings.TrimPrefix(text, "*")
	if i := strings.IndexByte(text, '['); i >= 0 {
		text = text[:i]
	}
	return text
}
