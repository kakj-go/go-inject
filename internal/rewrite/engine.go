package rewrite

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"github.com/dave/dst/dstutil"
)

type parsedFile struct {
	path        string
	ast         *ast.File
	file        *dst.File
	dec         *decorator.Decorator
	fset        *token.FileSet
	imports     map[string]string
	changed     bool
	additionKey string
}

type template struct {
	rule  Rule
	file  *parsedFile
	id    string
	order int
}

type engine struct {
	declarationsOnly bool
	importPath       string
	sources          []*parsedFile
	rules            []*template
	types            map[string]*typeDecl
	values           map[string]*valueDecl
	functions        map[string][]*functionDecl
	added            map[string]string
	imports          map[string]bool
	result           *Result
}

// Package applies all selected rules atomically in memory. It never changes the
// input buffers or writes files. A selected rule that does not match is an error.
func Package(importPath string, sources []Source, rules []Rule) (result *Result, err error) {
	return transform(importPath, sources, rules, false)
}

// Declarations prepares the actual target type environment before signatures
// are bound, without injecting function bodies or guessing type equivalence.
func Declarations(importPath string, sources []Source, rules []Rule) (*Result, error) {
	return transform(importPath, sources, rules, true)
}

func transform(importPath string, sources []Source, rules []Rule, declarationsOnly bool) (result *Result, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("rewrite %s: invalid source transformation: %v", importPath, recovered)
		}
	}()
	e := &engine{importPath: importPath, types: map[string]*typeDecl{}, values: map[string]*valueDecl{},
		functions: map[string][]*functionDecl{}, added: map[string]string{}, imports: map[string]bool{},
		result: &Result{Replacements: map[string][]byte{}, Additions: map[string][]byte{}}}
	e.declarationsOnly = declarationsOnly
	for _, source := range sources {
		f, parseErr := parse(source.Path, source.Data, source.ImportNames)
		if parseErr != nil {
			return nil, parseErr
		}
		e.sources = append(e.sources, f)
		if len(e.sources) > 1 && f.file.Name.Name != e.sources[0].file.Name.Name {
			return nil, fmt.Errorf("rewrite %s: sources contain different Go packages", importPath)
		}
	}
	if len(e.sources) == 0 && len(rules) != 0 {
		return nil, fmt.Errorf("rewrite %s: no target source files", importPath)
	}
	sort.Slice(e.sources, func(i, j int) bool { return e.sources[i].path < e.sources[j].path })
	if err = e.indexTargets(); err != nil {
		return nil, err
	}
	seen := map[string]string{}
	for _, rule := range rules {
		f, parseErr := parse(rule.Path, rule.Source, rule.ImportNames)
		if parseErr != nil {
			return nil, parseErr
		}
		id := rule.ID
		if id == "" {
			id = rule.Provider + ":" + rule.Target + ":" + filepath.Base(rule.Path)
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(rule.Source))
		if previous, exists := seen[id]; exists {
			if previous != digest {
				return nil, fmt.Errorf("rewrite: conflicting sources for rule %s", id)
			}
			continue
		}
		seen[id] = digest
		e.rules = append(e.rules, &template{rule: rule, file: f, id: id})
	}
	sort.Slice(e.rules, func(i, j int) bool { return e.rules[i].id < e.rules[j].id })
	// Register additions first, so templates may reference helpers from any file.
	for _, rule := range e.rules {
		if err = e.applyDeclarations(rule); err != nil {
			return nil, err
		}
	}
	for _, rule := range e.rules {
		if declarationsOnly {
			break
		}
		if err = e.applyFunctions(rule); err != nil {
			return nil, err
		}
	}
	if err = e.finishFunctions(); err != nil {
		return nil, err
	}
	for _, source := range e.sources {
		if !source.changed {
			continue
		}
		pruneImports(source)
		data, printErr := render(source.file)
		if printErr != nil {
			return nil, fmt.Errorf("rewrite %s: %w", source.path, printErr)
		}
		if source.additionKey != "" {
			e.result.Additions[source.additionKey] = data
		} else {
			e.result.Replacements[source.path] = data
		}
	}
	for name := range e.imports {
		e.result.Imports = append(e.result.Imports, name)
	}
	sort.Strings(e.result.Imports)
	sort.Slice(e.result.Matches, func(i, j int) bool {
		a, b := e.result.Matches[i], e.result.Matches[j]
		if a.Target != b.Target {
			return a.Target < b.Target
		}
		if a.Function != b.Function {
			return a.Function < b.Function
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Rule < b.Rule
	})
	sort.Slice(e.result.Links, func(i, j int) bool { return e.result.Links[i].Symbol < e.result.Links[j].Symbol })
	return e.result, nil
}

func parse(filename string, data []byte, names ...map[string]string) (*parsedFile, error) {
	fset := token.NewFileSet()
	a, err := parser.ParseFile(fset, filename, data, parser.ParseComments|parser.AllErrors)
	if err != nil {
		return nil, fmt.Errorf("rewrite parse %s: %w", filename, err)
	}
	d := decorator.NewDecorator(fset)
	f, err := d.DecorateFile(a)
	if err != nil {
		return nil, fmt.Errorf("rewrite decorate %s: %w", filename, err)
	}
	p := &parsedFile{path: filename, ast: a, file: f, dec: d, fset: fset, imports: map[string]string{}}
	mapStatements(p)
	for _, imp := range f.Imports {
		name, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		alias := importName(name)
		if len(names) > 0 && names[0][name] != "" {
			alias = names[0][name]
		}
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		p.imports[alias] = name
	}
	return p, nil
}

func render(file *dst.File) ([]byte, error) {
	var buffer bytes.Buffer
	if err := decorator.Fprint(&buffer, file); err != nil {
		return nil, err
	}
	data := normalizeLineDirectives(buffer.Bytes())
	if _, err := parser.ParseFile(token.NewFileSet(), "generated.go", data, parser.AllErrors); err != nil {
		return nil, fmt.Errorf("generated invalid Go: %w", err)
	}
	return data, nil
}

// The Go compiler recognizes //line only at column one. The decorated printer
// treats comments as ordinary decorations and may indent them or put them after
// an opening brace. Normalize actual comment tokens, never string contents.
func normalizeLineDirectives(data []byte) []byte {
	fset := token.NewFileSet()
	file := fset.AddFile("generated.go", -1, len(data))
	var scan scanner.Scanner
	scan.Init(file, data, nil, scanner.ScanComments)
	type edit struct {
		start, end int
		text       []byte
	}
	var edits []edit
	for {
		position, kind, literal := scan.Scan()
		if kind == token.EOF {
			break
		}
		if kind != token.COMMENT || !strings.HasPrefix(literal, "//line ") {
			continue
		}
		offset := file.Offset(position)
		start := bytes.LastIndexByte(data[:offset], '\n') + 1
		if start == offset {
			continue
		}
		if len(bytes.TrimSpace(data[start:offset])) == 0 {
			edits = append(edits, edit{start: start, end: offset})
		} else {
			edits = append(edits, edit{start: offset, end: offset, text: []byte{'\n'}})
		}
	}
	for i := len(edits) - 1; i >= 0; i-- {
		edit := edits[i]
		next := make([]byte, 0, len(data)+len(edit.text))
		next = append(next, data[:edit.start]...)
		next = append(next, edit.text...)
		next = append(next, data[edit.end:]...)
		data = next
	}
	return data
}

func importName(importPath string) string {
	name := path.Base(importPath)
	if len(name) > 1 && name[0] == 'v' {
		if _, err := strconv.Atoi(name[1:]); err == nil {
			name = path.Base(path.Dir(importPath))
		}
	}
	if i := strings.LastIndex(name, ".v"); i > 0 {
		if _, err := strconv.Atoi(name[i+2:]); err == nil {
			name = name[:i]
		}
	}
	return strings.ReplaceAll(name, "-", "_")
}

func directive(node dst.Node, name string) (string, bool) {
	for _, comment := range node.Decorations().Start {
		line := strings.TrimSpace(comment)
		prefix := "//inject:" + name
		if line == prefix {
			return "", true
		}
		if strings.HasPrefix(line, prefix+" ") {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix)), true
		}
	}
	return "", false
}

func order(node dst.Node) (int, error) {
	value, exists := directive(node, "order")
	if !exists {
		return 0, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid //inject:order %q", value)
	}
	return n, nil
}

func stripDirectives(node dst.Node) {
	dst.Inspect(node, func(n dst.Node) bool {
		if n == nil {
			return true
		}
		d := n.Decorations()
		for _, list := range []*dst.Decorations{&d.Start, &d.End} {
			filtered := (*list)[:0]
			for _, comment := range *list {
				if !strings.HasPrefix(strings.TrimSpace(comment), "//inject:") {
					filtered = append(filtered, comment)
				}
			}
			*list = filtered
		}
		return true
	})
}

func (e *engine) targetFile(rule *template) (*parsedFile, error) {
	if rule.rule.Target == "main" {
		for _, f := range e.sources {
			for _, d := range f.file.Decls {
				if fn, ok := d.(*dst.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "main" {
					return f, nil
				}
			}
		}
		return e.sources[0], nil
	}
	if rule.rule.Target != e.importPath {
		return nil, fmt.Errorf("rule %s: target package %s does not match %s", rule.id, rule.rule.Target, e.importPath)
	}
	if rule.rule.File == "" {
		// Package-level target: matching happens across the whole package.
		return nil, nil
	}
	var matched []*parsedFile
	for _, file := range e.sources {
		if rule.rule.File == filepath.Base(file.path) {
			matched = append(matched, file)
		}
	}
	if len(matched) != 1 {
		return nil, fmt.Errorf("rule %s (%s): target %s matched %d files in %s", rule.id, rule.rule.Path, rule.rule.Target+"/"+rule.rule.File, len(matched), e.importPath)
	}
	return matched[0], nil
}

// scopeName names the search scope in errors: the matched file for file
// targets, or the package import path for package-level targets.
func (e *engine) scopeName(rule *template, file *parsedFile) string {
	if file != nil {
		return file.path
	}
	return e.importPath
}

func (e *engine) record(rule *template, file *parsedFile, name string, order int) {
	e.result.Matches = append(e.result.Matches, Match{Rule: rule.id, Target: file.path, Function: name, Order: order})
	file.changed = true
}

func sourceMapping(file *parsedFile, node dst.Node) string {
	a := file.dec.Ast.Nodes[node]
	if a == nil {
		return ""
	}
	p := file.fset.Position(a.Pos())
	filename := strings.ReplaceAll(p.Filename, "\\", "/")
	if strings.ContainsAny(filename, "\n\r") {
		return ""
	}
	return fmt.Sprintf("//line %s:%d", filename, p.Line)
}

func mapStatements(file *parsedFile) {
	dstutil.Apply(file.file, func(cursor *dstutil.Cursor) bool {
		node := cursor.Node()
		if _, ok := node.(dst.Stmt); !ok {
			return true
		}
		if _, block := node.(*dst.BlockStmt); block {
			return true
		}
		switch cursor.Parent().(type) {
		case *dst.BlockStmt, *dst.CaseClause, *dst.CommClause, *dst.LabeledStmt:
			if mapping := sourceMapping(file, node); mapping != "" {
				node.Decorations().Start.Append(mapping)
			}
		}
		return true
	}, nil)
}
