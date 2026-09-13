package build

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kakj-go/go-inject/internal/project"
	"github.com/kakj-go/go-inject/internal/rewrite"
)

func option(args []string, key string) string {
	for i, a := range args {
		if a == key && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, key+"=") {
			return strings.TrimPrefix(a, key+"=")
		}
	}
	return ""
}
func delegate(ctx context.Context, args []string) error {
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func Worker(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("internal tool invocation requires the Go tool path")
	}
	s, e := ReadSession(os.Getenv(SessionEnv))
	if e != nil {
		return fmt.Errorf("go-inject internal session: %w; invoke the public tool with go build/test -toolexec=go-inject", e)
	}
	for _, a := range args[1:] {
		if a == "-V=full" {
			out, e := exec.CommandContext(ctx, args[0], "-V=full").Output()
			if e != nil {
				return e
			}
			fp := s.ToolFingerprint
			if fp == "" {
				fp = s.Fingerprint
			}
			fmt.Printf("%s go-inject=%s\n", bytes.TrimSpace(out), fp)
			return nil
		}
	}
	kind := strings.TrimSuffix(filepath.Base(args[0]), ".exe")
	if s.Native && (kind == "compile" || kind == "cgo") {
		args, e = s.nativeBaselineArgs(args, kind)
		if e != nil {
			return e
		}
	}
	if kind == "compile" {
		args, e = s.compile(ctx, args)
		if e != nil {
			return e
		}
	}
	if kind == "link" {
		if e = s.link(ctx, args); e != nil {
			return e
		}
	}
	if e = delegate(ctx, args); e != nil {
		return e
	}
	if kind == "compile" {
		identity := os.Getenv("TOOLEXEC_IMPORTPATH")
		if identity == "" {
			identity = option(args, "-p")
		}
		path := filepath.Join(s.Dir, "records", hash([]byte(identity))+".json")
		if data, err := os.ReadFile(path); err == nil {
			var r Record
			if err = json.Unmarshal(data, &r); err != nil {
				return err
			}
			if err := s.writeRecord(r, true); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if s.Native && (kind == "link" || kind == "compile" && strings.Split(os.Getenv("TOOLEXEC_IMPORTPATH"), " [")[0] == s.RootPath) {
		if err := s.Finish(); err != nil {
			return err
		}
		return s.Complete()
	}
	return nil
}

func (s *Session) compile(ctx context.Context, args []string) ([]string, error) {
	pkg := os.Getenv("TOOLEXEC_IMPORTPATH")
	if pkg == "" {
		pkg = option(args, "-p")
	}
	identity := pkg
	pkg = strings.Split(pkg, " [")[0]
	main := option(args, "-p") == "main" && (pkg == s.RootPath || pkg == s.RootPath+".test" || strings.HasSuffix(pkg, ".test"))
	rs := s.RulesFor(pkg)
	if main {
		rs = append(rs, s.RulesFor("main")...)
	}
	if len(rs) == 0 && !main {
		return args, nil
	}
	var sources []rewrite.Source
	indices := map[string]int{}
	names := s.Names()
	if p := s.Packages[pkg]; p != nil {
		names = project.SourceImports(p, names)
	}
	for i, a := range args {
		if !strings.HasSuffix(a, ".go") {
			continue
		}
		data, e := os.ReadFile(a)
		if e != nil {
			return nil, e
		}
		logical := a
		for orig, physical := range s.Overlay {
			if physical == a {
				logical = orig
				break
			}
		}
		base := filepath.Base(logical)
		if strings.HasSuffix(base, ".cgo1.go") {
			base = strings.TrimSuffix(base, ".cgo1.go") + ".go"
		}
		if strings.HasSuffix(base, ".cover.go") {
			base = strings.TrimSuffix(base, ".cover.go") + ".go"
		}
		if p := s.Packages[pkg]; p != nil {
			logical = filepath.Join(p.Dir, base)
		}
		sources = append(sources, rewrite.Source{Path: logical, Data: data, ImportNames: names})
		indices[logical] = i
	}
	result, e := rewrite.Package(pkg, sources, rs)
	if e != nil {
		return nil, fmt.Errorf("entry %s, target %s version %q: %w", s.RootPath, pkg, packageVersion(s.Packages[pkg]), e)
	}
	// Main additions from selected non-main variants retain their file ownership.
	if main {
		records, e := s.records()
		if e != nil {
			return nil, e
		}
		if result.Additions == nil {
			result.Additions = map[string][]byte{}
		}
		if s.Native && len(s.Needs) > 0 {
			var bootstrap strings.Builder
			bootstrap.WriteString("package main\nimport (\n")
			for _, dep := range s.Needs {
				if canSeedImport(s.RootPath, dep) {
					fmt.Fprintf(&bootstrap, "_ %q\n", dep)
				}
			}
			bootstrap.WriteString(")\n")
			result.Additions["goinject_dependencies.go"] = []byte(bootstrap.String())
		}
		for _, r := range records {
			if r.Package == "main" || r.Package == pkg {
				continue
			}
			for name, b := range r.Result.Additions {
				if strings.HasPrefix(filepath.ToSlash(name), "main/") {
					result.Additions[filepath.Base(name)] = b
				}
			}
			result.Imports = append(result.Imports, r.Result.Imports...)
		}
	}
	outDir := filepath.Dir(option(args, "-o"))
	if outDir == "." {
		return nil, fmt.Errorf("compile output path missing")
	}
	for original, data := range result.Replacements {
		i, ok := indices[original]
		if !ok {
			return nil, fmt.Errorf("rewriter returned unknown source %s", original)
		}
		dest := filepath.Join(outDir, "goinject_"+hash([]byte(original))[:12]+".go")
		if e = os.WriteFile(dest, data, 0600); e != nil {
			return nil, e
		}
		args[i] = dest
	}
	keys := make([]string, 0, len(result.Additions))
	for name := range result.Additions {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		if strings.HasPrefix(filepath.ToSlash(name), "main/") {
			if !main {
				continue
			}
		}
		dest := filepath.Join(outDir, "goinject_add_"+hash([]byte(name))[:12]+".go")
		if e = os.WriteFile(dest, result.Additions[name], 0600); e != nil {
			return nil, e
		}
		args = append(args, dest)
	}
	if e = s.imports(ctx, option(args, "-importcfg"), scopeImports(result, main), pkg); e != nil {
		return nil, e
	}
	if e = s.writeRecord(Record{Package: identity, Sources: sources, Result: result}, false); e != nil {
		return nil, e
	}
	return args, nil
}

// Main-routed additions must not create dependency edges on their source target.
func scopeImports(r *rewrite.Result, includeMain bool) []string {
	set := map[string]bool{}
	read := func(name string, b []byte) {
		n, e := parser.ParseFile(token.NewFileSet(), name, b, parser.ImportsOnly)
		if e != nil {
			return
		}
		for _, i := range n.Imports {
			p, e := strconv.Unquote(i.Path.Value)
			if e == nil {
				set[p] = true
			}
		}
	}
	for name, b := range r.Replacements {
		read(name, b)
	}
	for name, b := range r.Additions {
		if includeMain || !strings.HasPrefix(filepath.ToSlash(name), "main/") {
			read(name, b)
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func mainImports(r *rewrite.Result) []string {
	copy := &rewrite.Result{Additions: map[string][]byte{}}
	for name, data := range r.Additions {
		if strings.HasPrefix(filepath.ToSlash(name), "main/") {
			copy.Additions[name] = data
		}
	}
	return scopeImports(copy, true)
}

func (s *Session) canonicalImport(caller, name string) string {
	caller = strings.Split(caller, " [")[0]
	if p := s.Packages[caller]; p != nil {
		if canonical := project.CanonicalImport(p, name); canonical != name {
			return canonical
		}
	}
	if p := s.Packages[caller]; p != nil && p.Standard && !strings.HasPrefix(name, "vendor/") {
		if q := s.Packages["vendor/"+name]; q != nil && q.Standard {
			return "vendor/" + name
		}
		if st, e := os.Stat(filepath.Join(s.Env.GOROOT, "src", "vendor", filepath.FromSlash(name))); e == nil && st.IsDir() {
			return "vendor/" + name
		}
	}
	return name
}

func readImportcfg(p string) (map[string]string, []byte, error) {
	b, e := os.ReadFile(p)
	if e != nil {
		return nil, nil, e
	}
	m := map[string]string{}
	aliases := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "importmap ") {
			pair := strings.SplitN(strings.TrimPrefix(line, "importmap "), "=", 2)
			if len(pair) == 2 {
				aliases[pair[0]] = pair[1]
			}
		}
		if strings.HasPrefix(line, "packagefile ") {
			pair := strings.SplitN(strings.TrimPrefix(line, "packagefile "), "=", 2)
			if len(pair) == 2 {
				m[pair[0]] = pair[1]
			}
		}
	}
	for raw, canonical := range aliases {
		if archive := m[canonical]; archive != "" {
			m[raw] = archive
		}
	}
	return m, b, nil
}
func (s *Session) imports(ctx context.Context, path string, imports []string, caller string) error {
	if len(imports) == 0 {
		return nil
	}
	if path == "" {
		return fmt.Errorf("missing importcfg for %s", caller)
	}
	known, b, e := readImportcfg(path)
	if e != nil {
		return e
	}
	var extra strings.Builder
	for _, p := range imports {
		if p == "unsafe" || p == "C" || p == caller || p == "" || known[p] != "" {
			continue
		}
		canonical := s.canonicalImport(caller, p)
		if canonical != p {
			fmt.Fprintf(&extra, "importmap %s=%s\n", p, canonical)
			p = canonical
			if known[p] != "" {
				continue
			}
		}
		exports, e := s.Exports(ctx, p, caller)
		if e != nil {
			return e
		}
		for _, q := range exports {
			if known[q.Base()] == "" {
				if _, e = os.Stat(q.Export); e != nil {
					return fmt.Errorf("dependency %s export not ready: %w", q.Base(), e)
				}
				known[q.Base()] = q.Export
				fmt.Fprintf(&extra, "packagefile %s=%s\n", q.Base(), q.Export)
			}
		}
		if known[p] == "" {
			return fmt.Errorf("dependency %s has no completed export", p)
		}
	}
	if extra.Len() == 0 {
		return nil
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	return os.WriteFile(path, append(b, []byte(extra.String())...), 0600)
}

func (s *Session) link(ctx context.Context, args []string) error {
	path := option(args, "-importcfg")
	if path == "" {
		return nil
	}
	rs, e := s.records()
	if e != nil {
		return e
	}
	needs := append([]string{}, s.Needs...)
	for _, r := range rs {
		for _, p := range scopeImports(r.Result, false) {
			needs = append(needs, s.canonicalImport(r.Package, p))
		}
		needs = append(needs, mainImports(r.Result)...)
		for _, l := range r.Result.Links {
			p, _, e := splitSymbol(l.Symbol)
			if e != nil {
				return e
			}
			needs = append(needs, p)
		}
	}
	return s.imports(ctx, path, needs, "")
}

func Inspect(dir string, asJSON bool) error {
	b, e := os.ReadFile(filepath.Join(dir, "report.json"))
	if e != nil {
		return e
	}
	if asJSON {
		fmt.Println(string(b))
		return nil
	}
	var r Report
	if e = json.Unmarshal(b, &r); e != nil {
		return e
	}
	fmt.Printf("Entry: %s\nGo: %s %s/%s\nPlan: %s\n", r.Entry, r.GoVersion, r.GOOS, r.GOARCH, r.Fingerprint)
	for _, rule := range r.Rules {
		fmt.Printf("%s  %s -> %s %s\n", rule.State, rule.Rule, rule.Target, rule.Reason)
	}
	for _, p := range r.Packages {
		fmt.Printf("\nPackage %s\n", p.Package)
		for _, m := range p.Result.Matches {
			fmt.Printf("  order=%d %s -> %s\n", m.Order, m.Rule, m.Function)
		}
		for name, data := range p.Result.Replacements {
			fmt.Printf("\n--- %s (generated) ---\n%s\n", name, data)
		}
		for name, data := range p.Result.Additions {
			fmt.Printf("\n--- %s (added) ---\n%s\n", name, data)
		}
	}
	return nil
}
