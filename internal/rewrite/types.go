package rewrite

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/dave/dst"
)

type typeContext struct {
	file     *parsedFile
	pkg      string
	params   map[string]string
	aliases  map[string]*typeDecl
	visiting map[string]bool
}

func (e *engine) typeContext(file *parsedFile) *typeContext {
	c := &typeContext{file: file, pkg: e.importPath, params: map[string]string{}, aliases: map[string]*typeDecl{}, visiting: map[string]bool{}}
	for name, decl := range e.types {
		if decl.spec.Assign {
			c.aliases[name] = decl
		}
	}
	return c
}

func (c *typeContext) expr(expr dst.Expr) (string, error) {
	if expr == nil {
		return "", nil
	}
	switch n := expr.(type) {
	case *dst.Ident:
		if name, exists := c.params[n.Name]; exists {
			return name, nil
		}
		if alias, exists := c.aliases[n.Name]; exists && !c.visiting[n.Name] {
			c.visiting[n.Name] = true
			previousFile := c.file
			c.file = alias.file
			result, err := c.expr(alias.spec.Type)
			c.file = previousFile
			delete(c.visiting, n.Name)
			return result, err
		}
		if obj := types.Universe.Lookup(n.Name); obj != nil {
			if named, ok := obj.(*types.TypeName); ok {
				// TypeString's spelling of the predeclared any alias differs
				// between supported Go series; normalize the language identity.
				if n.Name == "any" {
					return "interface{}", nil
				}
				if basic, ok := types.Unalias(named.Type()).(*types.Basic); ok {
					return types.Typ[basic.Kind()].Name(), nil
				}
				return types.TypeString(types.Unalias(named.Type()), func(p *types.Package) string { return p.Path() }), nil
			}
		}
		return c.pkg + "." + n.Name, nil
	case *dst.SelectorExpr:
		id, ok := n.X.(*dst.Ident)
		if !ok {
			return "", fmt.Errorf("unsupported qualified type %T", n.X)
		}
		pkg, exists := c.file.imports[id.Name]
		if !exists {
			return "", fmt.Errorf("unknown package qualifier %s", id.Name)
		}
		return pkg + "." + n.Sel.Name, nil
	case *dst.StarExpr:
		x, err := c.expr(n.X)
		return "*" + x, err
	case *dst.ArrayType:
		x, err := c.expr(n.Elt)
		if err != nil {
			return "", err
		}
		if n.Len == nil {
			return "[]" + x, nil
		}
		length, err := c.constant(n.Len)
		return "[" + length + "]" + x, err
	case *dst.Ellipsis:
		x, err := c.expr(n.Elt)
		return "..." + x, err
	case *dst.MapType:
		key, err := c.expr(n.Key)
		if err != nil {
			return "", err
		}
		value, err := c.expr(n.Value)
		return "map[" + key + "]" + value, err
	case *dst.ChanType:
		x, err := c.expr(n.Value)
		prefix := "chan "
		if n.Dir == dst.SEND {
			prefix = "chan<- "
		}
		if n.Dir == dst.RECV {
			prefix = "<-chan "
		}
		return prefix + x, err
	case *dst.FuncType:
		return c.function(n)
	case *dst.IndexExpr:
		x, err := c.expr(n.X)
		if err != nil {
			return "", err
		}
		arg, err := c.expr(n.Index)
		return x + "[" + arg + "]", err
	case *dst.IndexListExpr:
		x, err := c.expr(n.X)
		if err != nil {
			return "", err
		}
		var args []string
		for _, arg := range n.Indices {
			s, err := c.expr(arg)
			if err != nil {
				return "", err
			}
			args = append(args, s)
		}
		return x + "[" + strings.Join(args, ",") + "]", nil
	case *dst.InterfaceType:
		var fields []string
		if n.Methods != nil {
			for _, field := range n.Methods.List {
				tp, err := c.expr(field.Type)
				if err != nil {
					return "", err
				}
				name := ""
				for _, id := range field.Names {
					name += id.Name + " "
				}
				fields = append(fields, name+tp)
			}
		}
		sort.Strings(fields)
		return "interface{" + strings.Join(fields, ";") + "}", nil
	case *dst.StructType:
		var fields []string
		if n.Fields != nil {
			for _, field := range n.Fields.List {
				tp, err := c.expr(field.Type)
				if err != nil {
					return "", err
				}
				tag := ""
				if field.Tag != nil {
					tag = " " + field.Tag.Value
				}
				if len(field.Names) == 0 {
					fields = append(fields, tp+tag)
				} else {
					for _, name := range field.Names {
						fields = append(fields, name.Name+" "+tp+tag)
					}
				}
			}
		}
		return "struct{" + strings.Join(fields, ";") + "}", nil
	case *dst.ParenExpr:
		return c.expr(n.X)
	case *dst.UnaryExpr:
		if n.Op != token.TILDE {
			return "", fmt.Errorf("unsupported type operator %s", n.Op)
		}
		x, err := c.expr(n.X)
		return "~" + x, err
	case *dst.BinaryExpr:
		if n.Op != token.OR {
			return "", fmt.Errorf("unsupported type operator %s", n.Op)
		}
		x, err := c.expr(n.X)
		if err != nil {
			return "", err
		}
		y, err := c.expr(n.Y)
		if x > y {
			x, y = y, x
		}
		return x + "|" + y, err
	default:
		return "", fmt.Errorf("unsupported Go type syntax %T", expr)
	}
}

func (c *typeContext) constant(expr dst.Expr) (string, error) {
	switch n := expr.(type) {
	case *dst.BasicLit:
		if n.Kind == token.INT {
			if value, err := strconv.ParseUint(strings.ReplaceAll(n.Value, "_", ""), 0, 64); err == nil {
				return strconv.FormatUint(value, 10), nil
			}
		}
		return n.Value, nil
	case *dst.Ident:
		return c.pkg + "." + n.Name, nil
	case *dst.SelectorExpr:
		return c.expr(n)
	case *dst.ParenExpr:
		return c.constant(n.X)
	case *dst.BinaryExpr:
		x, err := c.constant(n.X)
		if err != nil {
			return "", err
		}
		y, err := c.constant(n.Y)
		return "(" + x + n.Op.String() + y + ")", err
	case *dst.UnaryExpr:
		x, err := c.constant(n.X)
		return n.Op.String() + x, err
	default:
		return "", fmt.Errorf("unsupported array length expression %T", expr)
	}
}

func flatten(fields *dst.FieldList) []*dst.Field {
	var result []*dst.Field
	if fields == nil {
		return result
	}
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			result = append(result, &dst.Field{Type: field.Type})
			continue
		}
		for _, name := range field.Names {
			result = append(result, &dst.Field{Names: []*dst.Ident{name}, Type: field.Type})
		}
	}
	return result
}

func (c *typeContext) bindTypeParameters(fields *dst.FieldList) {
	for i, field := range flatten(fields) {
		if len(field.Names) > 0 {
			c.params[field.Names[0].Name] = fmt.Sprintf("$%d", i)
		}
	}
}

func (c *typeContext) fields(fields *dst.FieldList) (string, error) {
	var result []string
	for _, field := range flatten(fields) {
		tp, err := c.expr(field.Type)
		if err != nil {
			return "", err
		}
		result = append(result, tp)
	}
	return strings.Join(result, ","), nil
}

func (c *typeContext) function(fn *dst.FuncType) (string, error) {
	c.bindTypeParameters(fn.TypeParams)
	params, err := c.fields(fn.Params)
	if err != nil {
		return "", err
	}
	results, err := c.fields(fn.Results)
	if err != nil {
		return "", err
	}
	constraints, err := c.fields(fn.TypeParams)
	if err != nil {
		return "", err
	}
	return "func[" + constraints + "](" + params + ")(" + results + ")", nil
}

func receiverName(fn *dst.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	var expr dst.Expr = fn.Recv.List[0].Type
	for {
		switch n := expr.(type) {
		case *dst.StarExpr:
			expr = n.X
		case *dst.IndexExpr:
			expr = n.X
		case *dst.IndexListExpr:
			expr = n.X
		case *dst.Ident:
			return n.Name
		default:
			return "?"
		}
	}
}

func functionName(fn *dst.FuncDecl) string {
	receiver := receiverName(fn)
	if receiver == "" {
		return fn.Name.Name
	}
	return receiver + "." + fn.Name.Name
}

// clone keeps parser object identity: dst.Clone intentionally discards objects,
// but injection needs it to distinguish captured parameters from shadowed locals.
func clone(node dst.Node) dst.Node {
	copy := dst.Clone(node)
	var objects []*dst.Object
	dst.Inspect(node, func(n dst.Node) bool {
		if id, ok := n.(*dst.Ident); ok {
			objects = append(objects, id.Obj)
		}
		return true
	})
	i := 0
	dst.Inspect(copy, func(n dst.Node) bool {
		if id, ok := n.(*dst.Ident); ok {
			id.Obj = objects[i]
			i++
		}
		return true
	})
	return copy
}
