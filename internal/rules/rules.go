// Package rules discovers static import registrations and selects whole-file variants.
package rules

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kakj-go/go-inject/internal/project"
	"github.com/kakj-go/go-inject/internal/rewrite"
	"golang.org/x/mod/semver"
)

type Status struct{ Rule, Target, Version, State, Reason string }
type File struct {
	Path, Package, Module, Target, TargetFile, ID, Version string
	Active                                                 bool
	Source                                                 []byte
}
type Set struct {
	Files    []rewrite.Rule
	Statuses []Status
	Packages map[string]*project.Package
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*$`)
var boundPattern = regexp.MustCompile(`^(<=|>=|<|>|=)(v[^\s]+)$`)

func Header(filename string, data []byte) (File, error) {
	f := File{Path: filename, Source: data}
	n, err := parser.ParseFile(token.NewFileSet(), filename, data, parser.PackageClauseOnly|parser.ParseComments)
	if err != nil {
		return f, err
	}
	seen := map[string]bool{}
	for _, g := range n.Comments {
		for _, c := range g.List {
			if c.Pos() >= n.Package {
				continue
			}
			s := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
			if !strings.HasPrefix(s, "inject:") {
				continue
			}
			v := strings.TrimSpace(strings.TrimPrefix(s, "inject:"))
			key := "target"
			parts := strings.Fields(v)
			switch {
			case len(parts) > 0 && parts[0] == "id":
				key = "id"
				f.ID = strings.TrimSpace(strings.TrimPrefix(v, "id"))
				if !idPattern.MatchString(f.ID) {
					return f, fmt.Errorf("%s: invalid inject:id %q", filename, f.ID)
				}
			case len(parts) > 0 && parts[0] == "version":
				key = "version"
				f.Version = strings.TrimSpace(strings.TrimPrefix(v, "version"))
				if f.Version == "" {
					return f, fmt.Errorf("%s: inject:version requires a condition", filename)
				}
				if _, err := VersionMatches("v1.0.0", f.Version); err != nil {
					return f, fmt.Errorf("%s: %w", filename, err)
				}
			case v == "main":
				f.Target = "main"
			case strings.HasSuffix(v, ".go"):
				if path.Clean(v) != v || strings.ContainsAny(v, "\\ \t\r\n") || path.IsAbs(v) || path.Base(v) == ".go" {
					return f, fmt.Errorf("%s: invalid target %q", filename, v)
				}
				if err := project.ValidateImportPath(path.Dir(v)); err != nil {
					return f, fmt.Errorf("%s: invalid target %q: %w", filename, v, err)
				}
				if strings.ContainsAny(path.Base(v), "*?[]:@") {
					return f, fmt.Errorf("%s: invalid target filename %q", filename, v)
				}
				f.Target = path.Dir(v)
				f.TargetFile = path.Base(v)
			default:
				// Package-level target: declarations match anywhere in the
				// target package instead of one named file. Bare single words
				// without a domain dot stay invalid so typos remain loud.
				if err := project.ValidateImportPath(v); err != nil {
					return f, fmt.Errorf("%s: invalid package target %q: %w", filename, v, err)
				}
				first := strings.SplitN(v, "/", 2)[0]
				if !strings.Contains(v, "/") && !strings.Contains(first, ".") {
					return f, fmt.Errorf("%s: invalid package target %q: use an import path or a package/file.go target", filename, v)
				}
				f.Target = v
			}
			if seen[key] {
				return f, fmt.Errorf("%s: duplicate inject:%s directive", filename, key)
			}
			seen[key] = true
		}
	}
	if f.Version != "" && f.ID == "" {
		return f, fmt.Errorf("%s: inject:version requires inject:id", filename)
	}
	if f.Target == "" && (f.ID != "" || f.Version != "") {
		return f, fmt.Errorf("%s: missing inject target", filename)
	}
	return f, nil
}

func VersionMatches(version, constraint string) (bool, error) {
	if constraint == "" {
		return true, nil
	}
	if len(strings.Fields(constraint)) == 0 {
		return false, fmt.Errorf("empty module version condition")
	}
	valid := semver.IsValid(version)
	matches := valid
	for _, part := range strings.Fields(constraint) {
		m := boundPattern.FindStringSubmatch(part)
		if m == nil || !semver.IsValid(m[2]) {
			return false, fmt.Errorf("invalid module version condition %q; use AND comparisons such as >=v1.0.0 <v2.0.0", part)
		}
		cmp := semver.Compare(version, m[2])
		ok := false
		switch m[1] {
		case "<":
			ok = cmp < 0
		case "<=":
			ok = cmp <= 0
		case "=":
			ok = cmp == 0
		case ">=":
			ok = cmp >= 0
		case ">":
			ok = cmp > 0
		}
		matches = matches && ok
	}
	return matches, nil
}

// TargetPackage normalizes a rule target to its package import path. Targets
// are stored as "main" or a bare package path; file targets keep their file
// name in File.TargetFile instead.
func TargetPackage(target string) string {
	if target == "main" {
		return "main"
	}
	return target
}

func Registrations(env project.Env, dir string, flags []string, tests bool) ([]string, error) {
	return registrations(env, dir, flags, tests, "")
}

func registrations(env project.Env, dir string, flags []string, tests bool, packageName string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	on := env.Context(flags, true)
	off := env.Context(flags, false)
	var imports []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || (!tests && strings.HasSuffix(name, "_test.go")) {
			continue
		}
		a, e := on.MatchFile(dir, name)
		if e != nil {
			return nil, e
		}
		b, e := off.MatchFile(dir, name)
		if e != nil {
			return nil, e
		}
		if !a || b {
			continue
		}
		filename := filepath.Join(dir, name)
		data, e := os.ReadFile(filename)
		if e != nil {
			return nil, e
		}
		head, e := Header(filename, data)
		if e != nil {
			return nil, e
		}
		if head.Target != "" {
			continue
		}
		n, e := parser.ParseFile(token.NewFileSet(), filename, data, parser.ParseComments)
		if e != nil {
			return nil, e
		}
		if packageName != "" && n.Name.Name != packageName && !(tests && strings.HasSuffix(name, "_test.go") && n.Name.Name == packageName+"_test") {
			return nil, fmt.Errorf("%s: registration package %q does not match Go package %q", filename, n.Name.Name, packageName)
		}
		for _, d := range n.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.IMPORT {
				return nil, fmt.Errorf("%s: registration files may only contain imports; put templates in a rule package", filename)
			}
		}
		for _, imp := range n.Imports {
			if imp.Name == nil || imp.Name.Name != "_" {
				return nil, fmt.Errorf("%s: registration imports must use _", filename)
			}
			s, e := strconv.Unquote(imp.Path.Value)
			if e != nil {
				return nil, e
			}
			if e := project.ValidateImportPath(s); e != nil {
				return nil, fmt.Errorf("%s: %w", filename, e)
			}
			imports = append(imports, s)
		}
	}
	sort.Strings(imports)
	unique := imports[:0]
	for _, item := range imports {
		if len(unique) == 0 || unique[len(unique)-1] != item {
			unique = append(unique, item)
		}
	}
	return unique, nil
}

func Load(ctx context.Context, env project.Env, root *project.Package, business map[string]*project.Package, flags []string, tests bool) (*Set, error) {
	s := &Set{Packages: map[string]*project.Package{}}
	registrationGraph := map[string][]string{}
	queue, err := registrations(env, root.Dir, flags, tests, root.Name)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var files []File
	regFlags := project.Remove(flags, "tags", true)
	regFlags = append(regFlags, "-tags="+strings.Join(env.Context(flags, true).BuildTags, ","))
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		p, e := project.Metadata(ctx, env.Dir, regFlags, name)
		if e != nil {
			return nil, e
		}
		s.Packages[p.ImportPath] = p
		children, e := registrations(env, p.Dir, flags, false, p.Name)
		if e != nil {
			return nil, e
		}
		registrationGraph[name] = children
		queue = append(queue, children...)
		entries, e := os.ReadDir(p.Dir)
		if e != nil {
			return nil, e
		}
		count := 0
		for _, entry := range entries {
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			filename := filepath.Join(p.Dir, entry.Name())
			data, e := os.ReadFile(filename)
			if e != nil {
				return nil, e
			}
			f, e := Header(filename, data)
			if e != nil {
				return nil, e
			}
			if f.Target == "" {
				continue
			}
			count++
			f.Package = p.ImportPath
			if p.Module != nil {
				f.Module = p.Module.Path
			} else {
				return nil, fmt.Errorf("rule package %s has no module identity", name)
			}
			bc := env.Context(flags, true)
			f.Active, e = bc.MatchFile(p.Dir, entry.Name())
			if e != nil {
				return nil, e
			}
			files = append(files, f)
		}
		if count == 0 && len(children) == 0 {
			return nil, fmt.Errorf("%s: registered package contains no inject templates or registrations", name)
		}
	}
	if err := checkRegistrationCycles(registrationGraph); err != nil {
		return nil, err
	}
	groups := map[string][]File{}
	for _, f := range files {
		key := f.Module + ":" + f.ID + ":" + TargetPackage(f.Target)
		if f.ID == "" {
			key = f.Package + ":" + filepath.Base(f.Path)
		}
		groups[key] = append(groups[key], f)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		group := groups[key]
		target := TargetPackage(group[0].Target)
		p := business[target]
		version := ""
		if p != nil {
			version = p.Version()
		}
		if target != "main" && p == nil {
			s.Statuses = append(s.Statuses, Status{Rule: key, Target: target, State: "not-applicable", Reason: "target is not in the entry business dependency graph"})
			continue
		}
		if p != nil && p.Standard {
			for _, f := range group {
				if f.Version != "" {
					return nil, fmt.Errorf("%s: standard-library targets use Go build tags, not inject:version", f.Path)
				}
			}
		}
		var selected []File
		for _, f := range group {
			if !f.Active {
				continue
			}
			if f.Version != "" && version == "" {
				return nil, fmt.Errorf("%s: target %s version unknown (local replacements have no verified version); cannot evaluate %s", f.Path, target, f.Version)
			}
			ok, e := VersionMatches(version, f.Version)
			if e != nil {
				return nil, e
			}
			if ok {
				selected = append(selected, f)
			}
		}
		if len(selected) != 1 {
			paths := make([]string, 0, len(group))
			for _, f := range group {
				paths = append(paths, f.Path+" ["+f.Version+"]")
			}
			return nil, fmt.Errorf("rule %s: target %s version %q: expected exactly one applicable implementation, got %d; sources: %s", key, target, version, len(selected), strings.Join(paths, ", "))
		}
		f := selected[0]
		s.Files = append(s.Files, rewrite.Rule{Path: f.Path, Target: f.Target, File: f.TargetFile, Provider: f.Package, ID: key, Source: f.Source})
		s.Statuses = append(s.Statuses, Status{Rule: key, Target: target, Version: version, State: "selected"})
	}
	return s, nil
}

func checkRegistrationCycles(graph map[string][]string) error {
	states := map[string]uint8{}
	var stack []string
	var visit func(string) error
	visit = func(name string) error {
		if states[name] == 2 {
			return nil
		}
		if states[name] == 1 {
			return fmt.Errorf("rule registration import cycle: %s", strings.Join(append(append([]string{}, stack...), name), " -> "))
		}
		states[name] = 1
		stack = append(stack, name)
		for _, child := range graph[name] {
			if err := visit(child); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		states[name] = 2
		return nil
	}
	var names []string
	for name := range graph {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}
