package semantic

import (
	"go/token"
	"go/types"
)

// go/packages may load a bridge's package in a separate query. Canonicalize
// named origins by their Go package/object identity before types.Identical;
// aliases, constant array lengths, method sets and generic constraints come
// from go/types, never from source spelling.
type normalizer struct {
	named         map[string]*types.Named
	substitutions map[*types.TypeParam]*types.TypeParam
}

func equivalent(a, b types.Type) bool {
	n := &normalizer{named: map[string]*types.Named{}, substitutions: map[*types.TypeParam]*types.TypeParam{}}
	left := n.typ(a)
	n.substitutions = map[*types.TypeParam]*types.TypeParam{}
	right := n.typ(b)
	return types.Identical(left, right)
}

func signature(tps []*types.TypeParam, p, r *types.Tuple, variadic bool) *types.Signature {
	n := &normalizer{named: map[string]*types.Named{}, substitutions: map[*types.TypeParam]*types.TypeParam{}}
	return n.sig(tps, p, r, variadic)
}

func (n *normalizer) sig(tps []*types.TypeParam, p, r *types.Tuple, variadic bool) *types.Signature {
	old := n.substitutions
	local := make(map[*types.TypeParam]*types.TypeParam, len(old)+len(tps))
	for k, v := range old {
		local[k] = v
	}
	n.substitutions = local
	copies := make([]*types.TypeParam, len(tps))
	for i, tp := range tps {
		copies[i] = types.NewTypeParam(types.NewTypeName(token.NoPos, nil, tp.Obj().Name(), nil), types.NewInterfaceType(nil, nil))
		local[tp] = copies[i]
	}
	for i, tp := range tps {
		copies[i].SetConstraint(n.typ(tp.Constraint()))
	}
	result := types.NewSignatureType(nil, nil, copies, n.tuple(p), n.tuple(r), variadic)
	n.substitutions = old
	return result
}

func (n *normalizer) tuple(t *types.Tuple) *types.Tuple {
	vars := make([]*types.Var, t.Len())
	for i := range vars {
		v := t.At(i)
		vars[i] = types.NewVar(token.NoPos, v.Pkg(), v.Name(), n.typ(v.Type()))
	}
	return types.NewTuple(vars...)
}

func (n *normalizer) typ(t types.Type) types.Type {
	t = types.Unalias(t)
	switch t := t.(type) {
	case *types.Basic:
		return t
	case *types.TypeParam:
		if v := n.substitutions[t]; v != nil {
			return v
		}
		return t
	case *types.Named:
		obj := t.Origin().Obj()
		key := obj.Name()
		if obj.Pkg() != nil {
			key = obj.Pkg().Path() + "." + key
		}
		canonical := n.named[key]
		if canonical == nil {
			canonical = t.Origin()
			n.named[key] = canonical
		}
		if t.TypeArgs().Len() == 0 {
			return canonical
		}
		args := make([]types.Type, t.TypeArgs().Len())
		for i := range args {
			args[i] = n.typ(t.TypeArgs().At(i))
		}
		instance, err := types.Instantiate(nil, canonical, args, false)
		if err != nil {
			return t
		}
		return instance
	case *types.Pointer:
		return types.NewPointer(n.typ(t.Elem()))
	case *types.Slice:
		return types.NewSlice(n.typ(t.Elem()))
	case *types.Array:
		return types.NewArray(n.typ(t.Elem()), t.Len())
	case *types.Map:
		return types.NewMap(n.typ(t.Key()), n.typ(t.Elem()))
	case *types.Chan:
		return types.NewChan(t.Dir(), n.typ(t.Elem()))
	case *types.Tuple:
		return n.tuple(t)
	case *types.Signature:
		return n.sig(params(t.TypeParams()), t.Params(), t.Results(), t.Variadic())
	case *types.Struct:
		fields := make([]*types.Var, t.NumFields())
		tags := make([]string, len(fields))
		for i := range fields {
			f := t.Field(i)
			fields[i] = types.NewField(token.NoPos, f.Pkg(), f.Name(), n.typ(f.Type()), f.Embedded())
			tags[i] = t.Tag(i)
		}
		return types.NewStruct(fields, tags)
	case *types.Interface:
		methods := make([]*types.Func, t.NumExplicitMethods())
		for i := range methods {
			f := t.ExplicitMethod(i)
			methods[i] = types.NewFunc(token.NoPos, f.Pkg(), f.Name(), n.typ(f.Type()).(*types.Signature))
		}
		embedded := make([]types.Type, t.NumEmbeddeds())
		for i := range embedded {
			embedded[i] = n.typ(t.EmbeddedType(i))
		}
		return types.NewInterfaceType(methods, embedded).Complete()
	case *types.Union:
		terms := make([]*types.Term, t.Len())
		for i := range terms {
			terms[i] = types.NewTerm(t.Term(i).Tilde(), n.typ(t.Term(i).Type()))
		}
		return types.NewUnion(terms)
	default:
		return t
	}
}
