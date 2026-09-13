// Package build coordinates reproducible Go toolchain sessions.
package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kakj-go/go-inject/internal/project"
	"github.com/kakj-go/go-inject/internal/rewrite"
	"github.com/kakj-go/go-inject/internal/rules"
	"github.com/kakj-go/go-inject/internal/vendorstate"
)

const SessionEnv = "GOINJECT_SESSION"

type Session struct {
	Dir, Fingerprint, Cache, Kind, RootPath, Endpoint, Token, Executable string
	Env                                                                  project.Env
	Root                                                                 *project.Package
	Packages                                                             map[string]*project.Package
	Rules                                                                []rewrite.Rule
	Statuses                                                             []rules.Status
	Flags                                                                []string
	EntryArgs                                                            []string
	Overlay                                                              map[string]string
	Needs                                                                []string
	UseCacheSnapshots                                                    bool
	Native                                                               bool
	ToolFingerprint                                                      string
	Business                                                             map[string]bool
}
type Record struct {
	Package  string
	Planning bool
	Sources  []rewrite.Source
	Result   *rewrite.Result
}
type Report struct {
	Session                                           string
	Format                                            int
	Entry, Kind, GoVersion, GOOS, GOARCH, Fingerprint string
	Rules                                             []rules.Status
	Packages                                          []Record
}

func hash(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func jsonWrite(path string, v any) error {
	data, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".goinject-write-*")
	if e != nil {
		return e
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(temp, path)
}
func (s *Session) Save() error { return jsonWrite(filepath.Join(s.Dir, "session.json"), s) }
func ReadSession(path string) (*Session, error) {
	data, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var s Session
	e = json.Unmarshal(data, &s)
	return &s, e
}
func (s *Session) Names() map[string]string {
	m := map[string]string{}
	for k, p := range s.Packages {
		m[k] = p.Name
	}
	return m
}
func VendorRoot(env project.Env) string {
	base := filepath.Dir(env.GOMOD)
	if env.GOWORK != "" && env.GOWORK != "off" {
		base = filepath.Dir(env.GOWORK)
	}
	return filepath.Join(base, "vendor")
}

func Sources(p *project.Package, overlay map[string]string, names map[string]string) ([]rewrite.Source, error) {
	names = project.SourceImports(p, names)
	var out []rewrite.Source
	for _, name := range append(append([]string{}, p.GoFiles...), p.CgoFiles...) {
		file := filepath.Join(p.Dir, name)
		if filepath.IsAbs(name) {
			file = name
		}
		b, e := project.Read(file, overlay)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		out = append(out, rewrite.Source{Path: file, Data: b, ImportNames: names})
	}
	return out, nil
}
func (s *Session) RulesFor(pkg string) []rewrite.Rule {
	var out []rewrite.Rule
	for _, r := range s.Rules {
		if rules.TargetPackage(r.Target) == pkg {
			out = append(out, r)
		}
	}
	return out
}

func Prepare(ctx context.Context, env project.Env, root *project.Package, flags []string, kind string) (s *Session, err error) {
	return prepare(ctx, env, root, flags, kind, false)
}

func PrepareNative(ctx context.Context, env project.Env, root *project.Package, flags []string, kind string) (*Session, error) {
	return prepare(ctx, env, root, flags, kind, true)
}

func prepare(ctx context.Context, env project.Env, root *project.Package, flags []string, kind string, native bool) (s *Session, err error) {
	dir, err := os.MkdirTemp("", "go-inject-")
	if err != nil {
		return nil, err
	}
	s = &Session{Dir: dir, Kind: kind, RootPath: root.Base(), Root: root, Env: env, Flags: append([]string{}, flags...), Overlay: map[string]string{}}
	s.Native = native
	s.EntryArgs = []string{root.Base()}
	if len(root.EntryFiles) > 0 {
		s.EntryArgs = append([]string{}, root.EntryFiles...)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	s.Executable, err = os.Executable()
	if err != nil {
		return nil, err
	}
	for i, a := range flags {
		if a == "-overlay" && i+1 < len(flags) {
			err = s.readOverlay(flags[i+1])
		}
		if strings.HasPrefix(a, "-overlay=") {
			err = s.readOverlay(strings.TrimPrefix(a, "-overlay="))
		}
		if err != nil {
			return nil, err
		}
	}
	if vendorstate.HasState(VendorRoot(env)) {
		m, e := vendorstate.Overlay(VendorRoot(env), filepath.Join(dir, "baseline"))
		if e != nil {
			return nil, e
		}
		for k, v := range m {
			s.Overlay[k] = v
		}
	}
	listFlags := project.ListFlags(flags)
	listFlags = project.Remove(listFlags, "overlay", true)
	if len(s.Overlay) > 0 {
		p := filepath.Join(dir, "baseline.json")
		if err = project.OverlayFile(p, s.Overlay); err != nil {
			return nil, err
		}
		listFlags = append(listFlags, "-overlay", p)
	}
	pkgs, err := project.List(ctx, env.Dir, listFlags, true, kind == "test", s.EntryArgs...)
	if err != nil {
		return nil, err
	}
	s.Packages = project.Index(pkgs)
	s.Business = make(map[string]bool, len(s.Packages))
	for name := range s.Packages {
		s.Business[name] = true
	}
	if kind == "test" {
		for _, p := range pkgs {
			if p.ForTest == root.Base() {
				s.Packages[p.Base()] = p
			}
		}
	}
	if p := s.Packages[root.Base()]; p != nil {
		s.Root = p
	}
	for _, p := range pkgs {
		if p.Error != nil {
			return nil, fmt.Errorf("business package %s: %s", p.ImportPath, p.Error.Err)
		}
	}
	set, err := rules.Load(ctx, env, s.Root, s.Packages, listFlags, kind == "test")
	if err != nil {
		return nil, err
	}
	s.Rules = set.Files
	s.Statuses = set.Statuses
	if kind == "vendor" {
		for _, rule := range s.Rules {
			target := rules.TargetPackage(rule.Target)
			p := s.Packages[target]
			if target == "main" || p == nil || p.Standard || p.Module == nil || p.Module.Main {
				return nil, fmt.Errorf("vendor does not support target %s (rule %s); use go build -toolexec=go-inject", rule.Target, rule.Path)
			}
		}
	}
	imports := map[string]bool{}
	for _, r := range s.Rules {
		n, e := parser.ParseFile(token.NewFileSet(), r.Path, r.Source, parser.ImportsOnly)
		if e != nil {
			return nil, e
		}
		for _, imp := range n.Imports {
			p, e := strconv.Unquote(imp.Path.Value)
			if e != nil {
				return nil, e
			}
			if p != "C" {
				imports[p] = true
			}
		}
	}
	for p := range imports {
		extra, e := project.List(ctx, env.Dir, listFlags, true, false, p)
		if e != nil {
			return nil, e
		}
		for _, q := range extra {
			if q.Error != nil {
				return nil, fmt.Errorf("rule dependency %s: %s", q.ImportPath, q.Error.Err)
			}
			if s.Packages[q.Base()] == nil {
				s.Packages[q.Base()] = q
			}
		}
	}
	names := s.Names()
	for i := range s.Rules {
		s.Rules[i].ImportNames = names
	}
	if err = s.bindRules(ctx, listFlags); err != nil {
		return nil, err
	}
	// All generated dependencies are seeded before Go builds its action graph.
	// The coordinator still resolves imports encountered during actual compilation.
	needs := map[string]bool{}
	targets := map[string]bool{}
	for _, r := range s.Rules {
		targets[rules.TargetPackage(r.Target)] = true
	}
	for target := range targets {
		var src []rewrite.Source
		if target == "main" {
			if kind == "test" {
				src = []rewrite.Source{{Path: filepath.Join(s.Root.Dir, "_testmain.go"), Data: []byte("package main\nfunc main() {}\n"), ImportNames: names}}
			} else {
				src, err = Sources(s.Root, s.Overlay, names)
				if err != nil {
					return nil, err
				}
			}
		} else {
			p := s.Packages[target]
			if p == nil {
				continue
			}
			src, err = Sources(p, s.Overlay, names)
			if err != nil {
				return nil, err
			}
		}
		bindingPath := target
		if target == "main" {
			bindingPath = s.RootPath
		}
		result, e := rewrite.Package(bindingPath, src, s.RulesFor(target))
		if e != nil {
			return nil, fmt.Errorf("target %s version %q: %w", target, packageVersion(s.Packages[target]), e)
		}
		originalImports := map[string]bool{}
		if p := s.Packages[target]; p != nil {
			for _, dep := range p.Imports {
				originalImports[dep] = true
				originalImports[s.canonicalImport(target, dep)] = true
			}
		}
		for _, p := range scopeImports(result, false) {
			canonical := s.canonicalImport(target, p)
			if !originalImports[canonical] {
				needs[canonical] = true
			}
		}
		for _, p := range mainImports(result) {
			needs[p] = true
		}
		for _, l := range result.Links {
			p, _, e := splitSymbol(l.Symbol)
			if e != nil {
				return nil, e
			}
			needs[p] = true
			if e = s.validateLink(ctx, l, listFlags); e != nil {
				return nil, e
			}
		}
		if err = s.writeRecord(Record{Package: target, Planning: true, Sources: src, Result: result}, false); err != nil {
			return nil, err
		}
	}
	delete(needs, "unsafe")
	delete(needs, "C")
	delete(needs, s.RootPath)
	for _, r := range s.Rules {
		delete(needs, r.Provider)
	}
	for p := range needs {
		if p == "" || strings.HasPrefix(p, "vendor/") {
			continue
		}
		s.Needs = append(s.Needs, p)
	}
	sort.Strings(s.Needs)
	if err = s.checkCycles(ctx, listFlags); err != nil {
		return nil, err
	}
	// Include local helper source changes; no ephemeral session paths enter the fingerprint.
	fp := sha256.New()
	envBytes, _ := json.Marshal(env)
	fp.Write(envBytes)
	fmt.Fprintf(fp, "format=1\nroot=%s\ndir=%s\nkind=%s\ngo=%s/%s/%s/%s\n", s.RootPath, env.Dir, kind, env.GOVERSION, env.GOOS, env.GOARCH, env.CGO_ENABLED)
	exeBytes, e := os.ReadFile(s.Executable)
	if e != nil {
		return nil, e
	}
	fp.Write([]byte(hash(exeBytes)))
	semantic := project.Remove(project.Remove(project.Remove(flags, "work", false), "o", true), "overlay", true)
	b, _ := json.Marshal(semantic)
	fp.Write(b)
	for _, r := range s.Rules {
		fmt.Fprintf(fp, "rule=%s/%s/%s\n", r.ID, r.Target, r.Path)
		fp.Write(r.Source)
	}
	keys := make([]string, 0, len(s.Packages))
	for k := range s.Packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := s.Packages[k]
		fmt.Fprintf(fp, "package=%s/%s/%s\n", k, p.Dir, p.Version())
		all := append(append(append(append(append(append([]string{}, p.GoFiles...), p.CgoFiles...), p.CFiles...), p.HFiles...), p.SFiles...), p.EmbedFiles...)
		sort.Strings(all)
		for _, f := range all {
			file := filepath.Join(p.Dir, f)
			if filepath.IsAbs(f) {
				file = f
			}
			data, e := project.Read(file, s.Overlay)
			if e == nil {
				fp.Write([]byte(f))
				fp.Write(data)
			}
		}
	}
	s.Fingerprint = hex.EncodeToString(fp.Sum(nil))
	cache, e := os.UserCacheDir()
	if e != nil {
		return nil, e
	}
	s.Cache = filepath.Join(cache, "go-inject", "v1", s.Fingerprint)
	if err = os.MkdirAll(s.Cache, 0700); err != nil {
		return nil, err
	}
	s.UseCacheSnapshots = s.SnapshotValid()
	if err = project.OverlayFile(filepath.Join(dir, "overlay.json"), s.Overlay); err != nil {
		return nil, err
	}
	return s, s.Save()
}

func canSeedImport(entry, dep string) bool {
	if strings.HasPrefix(dep, "internal/") || strings.HasPrefix(dep, "vendor/") {
		return false
	}
	if i := strings.LastIndex(dep, "/internal/"); i >= 0 {
		parent := dep[:i]
		return entry == parent || strings.HasPrefix(entry, parent+"/")
	}
	return true
}

func packageVersion(p *project.Package) string {
	if p == nil {
		return ""
	}
	return p.Version()
}
func (s *Session) readOverlay(p string) error {
	data, e := os.ReadFile(p)
	if e != nil {
		return e
	}
	var m struct{ Replace map[string]string }
	if e = json.Unmarshal(data, &m); e != nil {
		return e
	}
	for k, v := range m.Replace {
		if !filepath.IsAbs(k) {
			k = filepath.Join(s.Env.Dir, k)
		}
		if v != "" && !filepath.IsAbs(v) {
			v = filepath.Join(s.Env.Dir, v)
		}
		s.Overlay[k] = v
	}
	return nil
}
func (s *Session) writeRecord(r Record, cache bool) error {
	base := filepath.Join(s.Dir, "records")
	if r.Planning {
		base = filepath.Join(s.Dir, "plan-records")
	}
	if cache {
		base = filepath.Join(s.Cache, "records")
	}
	return jsonWrite(filepath.Join(base, hash([]byte(r.Package))+".json"), r)
}
func (s *Session) records() ([]Record, error) { return s.readRecords(true) }

func (s *Session) PlannedResults() (map[string]*rewrite.Result, error) {
	r, err := s.readRecords(true)
	if err != nil {
		return nil, err
	}
	out := map[string]*rewrite.Result{}
	for _, record := range r {
		out[record.Package] = record.Result
	}
	return out, nil
}

func (s *Session) ReplicateRecord(from, identity string) error {
	data, err := os.ReadFile(filepath.Join(from, "records", hash([]byte(identity))+".json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var record Record
	if err = json.Unmarshal(data, &record); err != nil {
		return err
	}
	if err = s.writeRecord(record, false); err != nil {
		return err
	}
	return s.writeRecord(record, true)
}
func (s *Session) readRecords(includePlan bool) ([]Record, error) {
	m := map[string]Record{}
	var bases []string
	if includePlan {
		bases = append(bases, filepath.Join(s.Dir, "plan-records"))
	}
	if s.Cache != "" && s.UseCacheSnapshots {
		bases = append(bases, filepath.Join(s.Cache, "records"))
	}
	bases = append(bases, filepath.Join(s.Dir, "records"))
	for _, base := range bases {
		entries, e := os.ReadDir(base)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			data, e := os.ReadFile(filepath.Join(base, entry.Name()))
			if e != nil {
				return nil, e
			}
			var r Record
			if e = json.Unmarshal(data, &r); e != nil {
				return nil, e
			}
			m[r.Package] = r
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []Record
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out, nil
}
func (s *Session) Finish() error {
	rs, e := s.readRecords(false)
	if e != nil {
		return e
	}
	// A main-only planning stub is not an actual compiler input.
	actual := rs[:0]
	for _, r := range rs {
		actual = append(actual, r)
		base := filepath.Join(s.Dir, "sources", hash([]byte(r.Package))[:12])
		for _, src := range r.Sources {
			if e = os.MkdirAll(filepath.Join(base, "original"), 0700); e != nil {
				return e
			}
			if e = os.WriteFile(filepath.Join(base, "original", filepath.Base(src.Path)), src.Data, 0600); e != nil {
				return e
			}
		}
		for name, data := range r.Result.Replacements {
			if e = os.MkdirAll(filepath.Join(base, "generated"), 0700); e != nil {
				return e
			}
			if e = os.WriteFile(filepath.Join(base, "generated", filepath.Base(name)), data, 0600); e != nil {
				return e
			}
		}
		for name, data := range r.Result.Additions {
			if e = os.MkdirAll(filepath.Join(base, "added"), 0700); e != nil {
				return e
			}
			if e = os.WriteFile(filepath.Join(base, "added", filepath.Base(name)), data, 0600); e != nil {
				return e
			}
		}
	}
	return jsonWrite(filepath.Join(s.Dir, "report.json"), Report{Session: s.Dir, Format: 1, Entry: s.RootPath, Kind: s.Kind, GoVersion: s.Env.GOVERSION, GOOS: s.Env.GOOS, GOARCH: s.Env.GOARCH, Fingerprint: s.Fingerprint, Rules: s.Statuses, Packages: actual})
}

func (s *Session) SnapshotValid() bool {
	data, e := os.ReadFile(filepath.Join(s.Cache, "complete.json"))
	if e != nil {
		return false
	}
	var sums map[string]string
	if json.Unmarshal(data, &sums) != nil {
		return false
	}
	for name, sum := range sums {
		if filepath.Base(name) != name {
			return false
		}
		b, e := os.ReadFile(filepath.Join(s.Cache, "records", name))
		if e != nil || hash(b) != sum {
			return false
		}
	}
	return len(sums) > 0
}
func (s *Session) Complete() error {
	entries, e := os.ReadDir(filepath.Join(s.Cache, "records"))
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	sums := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, e := os.ReadFile(filepath.Join(s.Cache, "records", entry.Name()))
		if e != nil {
			return e
		}
		sums[entry.Name()] = hash(b)
	}
	return jsonWrite(filepath.Join(s.Cache, "complete.json"), sums)
}

func (s *Session) checkCycles(ctx context.Context, flags []string) error {
	graph := map[string][]string{}
	for k, p := range s.Packages {
		graph[k] = append([]string{}, p.Imports...)
	}
	rs, e := s.records()
	if e != nil {
		return e
	}
	for _, r := range rs {
		if r.Package == "main" {
			continue
		}
		for _, p := range scopeImports(r.Result, false) {
			graph[r.Package] = append(graph[r.Package], s.canonicalImport(r.Package, p))
		}
	}
	state := map[string]int{}
	var stack []string
	var visit func(string) error
	visit = func(n string) error {
		if state[n] == 1 {
			return fmt.Errorf("injected import cycle: %s -> %s", strings.Join(stack, " -> "), n)
		}
		if state[n] == 2 {
			return nil
		}
		state[n] = 1
		stack = append(stack, n)
		for _, p := range graph[n] {
			if p == "C" {
				continue
			}
			if err := visit(p); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[n] = 2
		return nil
	}
	for n := range graph {
		if e = visit(n); e != nil {
			return e
		}
	}
	return nil
}

func splitSymbol(symbol string) (string, string, error) {
	i := strings.LastIndex(symbol, ".")
	if i < 1 || i == len(symbol)-1 || strings.ContainsAny(symbol[i+1:], "()*[]") {
		return "", "", fmt.Errorf("unsupported bridge symbol %q; beta supports package-level non-generic functions", symbol)
	}
	return symbol[:i], symbol[i+1:], nil
}
func (s *Session) validateLink(ctx context.Context, l rewrite.Link, flags []string) error {
	if l.Checked {
		return nil
	}
	p, name, e := splitSymbol(l.Symbol)
	if e != nil {
		return e
	}
	pkgs, e := project.List(ctx, s.Env.Dir, flags, true, false, p)
	if e != nil {
		return e
	}
	var target *project.Package
	for _, q := range pkgs {
		if s.Packages[q.Base()] == nil {
			s.Packages[q.Base()] = q
		}
		if q.Base() == p {
			target = q
		}
	}
	if target == nil || target.Error != nil {
		return fmt.Errorf("bridge %s: target package unavailable", l.Symbol)
	}
	src, e := Sources(target, s.Overlay, s.Names())
	if e != nil {
		return e
	}
	actual, e := rewrite.CanonicalSignature(p, src, name)
	if e != nil {
		return fmt.Errorf("bridge %s: %w", l.Symbol, e)
	}
	if actual != l.Signature {
		return fmt.Errorf("bridge %s signature mismatch: expected %s, got %s", l.Symbol, l.Signature, actual)
	}
	return nil
}
