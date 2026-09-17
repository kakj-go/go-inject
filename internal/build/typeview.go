package build

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kakj-go/go-inject/internal/project"
	"golang.org/x/mod/modfile"
)

func (s *Session) loadBridgePackages(ctx context.Context, flags []string) error {
	targets := map[string]bool{}
	for _, r := range s.Rules {
		file, e := parser.ParseFile(token.NewFileSet(), r.Path, r.Source, parser.ParseComments)
		if e != nil {
			return e
		}
		for _, g := range file.Comments {
			for _, c := range g.List {
				parts := strings.Fields(c.Text)
				if len(parts) == 3 && parts[0] == "//go:linkname" {
					// Self-linknames (//go:linkname X X) rename a symbol inside
					// the target package instead of bridging to another one.
					if parts[1] == parts[2] {
						continue
					}
					pkg, _, e := splitSymbol(parts[2])
					if e != nil {
						return e
					}
					targets[pkg] = true
				}
			}
		}
	}
	for target := range targets {
		if s.Packages[target] != nil {
			continue
		}
		list, e := project.List(ctx, s.Env.Dir, flags, true, false, target)
		if e != nil {
			return e
		}
		for _, p := range list {
			if p.Error != nil {
				return fmt.Errorf("bridge dependency %s: %s", p.Base(), p.Error.Err)
			}
			if s.Packages[p.Base()] == nil {
				s.Packages[p.Base()] = p
			}
		}
	}
	return nil
}

// Go disallows overlays beneath the active module cache. The type-checking
// view uses temporary module/work files pointing at already-resolved source
// directories and a separate cache boundary. Original module files, module
// cache sources and GOROOT are never modified or copied wholesale.
func (s *Session) typeEnvironment(flags []string) ([]string, []string, error) {
	// Coverage changes compiler inputs, not type identity. Asking go/packages
	// to build covered synthetic testmains can also produce invalid cache paths.
	flags = project.Remove(project.Remove(project.Remove(flags, "cover", false), "coverpkg", true), "covermode", true)
	cmd := project.Command(context.Background(), s.Env.Dir)
	env := append(cmd.Environ(), "GOTOOLCHAIN=local", "GOROOT="+s.Env.GOROOT, "GOMODCACHE="+filepath.Join(s.Dir, "type-cache"))
	mod, _ := project.Value(flags, "mod")
	vendor := mod == "vendor"
	if mod == "" {
		_, e := os.Stat(filepath.Join(VendorRoot(s.Env), "modules.txt"))
		vendor = e == nil
	}
	if vendor {
		return env, flags, nil
	}
	modules := map[string]*project.Module{}
	for _, p := range s.Packages {
		if p.Module != nil {
			modules[p.Module.Path] = p.Module
		}
	}
	keys := make([]string, 0, len(modules))
	for k := range modules {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	directory := func(m *project.Module) string {
		if m.Replace != nil && m.Replace.Dir != "" {
			return m.Replace.Dir
		}
		return m.Dir
	}
	// Directory replacements require a go.mod inside the target directory.
	// Legacy modules published before go.mod existed (github.com/pkg/errors
	// and friends) extract without one; the proxy serves their module file
	// separately, so resolving them normally is correct while replacing them
	// breaks. Such modules never carry overlays, so skipping their
	// replacement only means they resolve inside the type cache.
	replacable := func(m *project.Module) bool {
		dir := directory(m)
		if dir == "" {
			return false
		}
		_, err := os.Stat(filepath.Join(dir, "go.mod"))
		return err == nil
	}
	flags = project.Remove(project.Remove(flags, "modfile", true), "mod", true)
	if s.Env.GOWORK != "" && s.Env.GOWORK != "off" {
		work, e := modfile.ParseWork("types.work", []byte("go "+strings.TrimPrefix(s.Env.GOVERSION, "go")+"\n"), nil)
		if e != nil {
			return nil, nil, e
		}
		for _, key := range keys {
			m := modules[key]
			dir := directory(m)
			if dir == "" {
				continue
			}
			if !replacable(m) {
				continue
			}
			if m.Main {
				if e := work.AddUse(dir, ""); e != nil {
					return nil, nil, e
				}
			} else if e := work.AddReplace(m.Path, "", dir, ""); e != nil {
				return nil, nil, e
			}
		}
		name := filepath.Join(s.Dir, "types.work")
		if e := os.WriteFile(name, modfile.Format(work.Syntax), 0600); e != nil {
			return nil, nil, e
		}
		return append(env, "GOWORK="+name), append(flags, "-mod=readonly"), nil
	}
	original := s.Env.GOMOD
	if custom, ok := project.Value(s.Flags, "modfile"); ok {
		original = custom
		if !filepath.IsAbs(original) {
			original = filepath.Join(s.Env.Dir, original)
		}
	}
	data, e := os.ReadFile(original)
	if e != nil {
		return nil, nil, e
	}
	file, e := modfile.Parse(original, data, nil)
	if e != nil {
		return nil, nil, e
	}
	for _, key := range keys {
		m := modules[key]
		if m.Main {
			continue
		}
		if !replacable(m) {
			continue
		}
		if e = file.AddReplace(m.Path, "", directory(m), ""); e != nil {
			return nil, nil, e
		}
	}
	data, e = file.Format()
	if e != nil {
		return nil, nil, e
	}
	name := filepath.Join(s.Dir, "types.mod")
	if e = os.WriteFile(name, data, 0600); e != nil {
		return nil, nil, e
	}
	if sums, e := os.ReadFile(strings.TrimSuffix(original, ".mod") + ".sum"); e == nil {
		if e = os.WriteFile(strings.TrimSuffix(name, ".mod")+".sum", sums, 0600); e != nil {
			return nil, nil, e
		}
	}
	return append(env, "GOWORK=off"), append(flags, "-modfile", name, "-mod=mod"), nil
}
