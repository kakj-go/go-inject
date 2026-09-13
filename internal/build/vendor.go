package build

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kakj-go/go-inject/internal/process"
	"github.com/kakj-go/go-inject/internal/project"
	"github.com/kakj-go/go-inject/internal/vendorstate"
)

func hasVendorState(e project.Env) bool { return vendorstate.HasState(VendorRoot(e)) }
func baselineOverlay(e project.Env, dest string) (map[string]string, error) {
	return vendorstate.Overlay(VendorRoot(e), dest)
}

func Vendor(ctx context.Context, dir string, args []string) error {
	restore := false
	var rest []string
	for _, a := range args {
		if a == "--restore" || a == "-restore" {
			restore = true
		} else {
			rest = append(rest, a)
		}
	}
	dir, flags, patterns, e := project.ResolveArgs(ctx, dir, rest)
	if e != nil {
		return e
	}
	env, e := project.Environment(ctx, dir)
	if e != nil {
		return e
	}
	vendorRoot := VendorRoot(env)
	if restore {
		return vendorstate.Restore(vendorRoot)
	}
	listFlags := project.ListFlags(flags)
	exists := false
	if st, e := os.Stat(vendorRoot); e == nil && st.IsDir() {
		exists = true
	}
	if !exists {
		listFlags = append(project.Remove(listFlags, "mod", true), "-mod=mod")
		flags = append(project.Remove(flags, "mod", true), "-mod=mod")
	}
	roots, e := project.List(ctx, env.Dir, listFlags, false, false, patterns...)
	if e != nil {
		return e
	}
	if len(roots) == 0 {
		return fmt.Errorf("no vendor entry packages")
	}
	var sessions []*Session
	defer func() {
		for _, s := range sessions {
			_ = os.RemoveAll(s.Dir)
		}
	}()
	for _, r := range roots {
		if r.Error != nil {
			return fmt.Errorf("entry %s: %s", r.Base(), r.Error.Err)
		}
		s, e := Prepare(ctx, env, r, flags, "vendor")
		if e != nil {
			return e
		}
		sessions = append(sessions, s)
		for _, rule := range s.Rules {
			target := strings.TrimSuffix(rule.Target, "/"+filepath.Base(rule.Target))
			p := s.Packages[target]
			if rule.Target == "main" || p == nil || p.Standard || p.Module == nil || p.Module.Main {
				return fmt.Errorf("vendor does not support target %s; use go-inject build", rule.Target)
			}
		}
		records, e := s.records()
		if e != nil {
			return e
		}
		for _, record := range records {
			for name := range record.Result.Additions {
				if strings.HasPrefix(filepath.ToSlash(name), "main/") {
					return fmt.Errorf("vendor does not support entry initialization in %s", record.Package)
				}
			}
		}
		if e = s.ValidateGenerated(ctx); e != nil {
			return e
		}
	}
	// Nothing is written to the project until every selected rule is validated.
	var stage string
	if !exists {
		stage, e = os.MkdirTemp(filepath.Dir(vendorRoot), ".goinject-vendor-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(stage)
		args := []string{"mod", "vendor", "-o", stage}
		if env.GOWORK != "" && env.GOWORK != "off" {
			args = []string{"work", "vendor", "-o", stage}
		}
		c := project.Command(ctx, env.Dir, args...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if e = process.Run(ctx, c); e != nil {
			return e
		}
	}
	outputs := map[string][]byte{}
	plan := vendorstate.Plan{Owners: map[string][]string{}, Inputs: map[string]vendorstate.Input{}}
	// Compare complete effective source for every shared dependency, including
	// packages that one entry leaves untouched.
	shared := map[string]map[string][]byte{}
	for _, s := range sessions {
		records, e := s.records()
		if e != nil {
			return e
		}
		changes := map[string]Record{}
		for _, r := range records {
			changes[r.Package] = r
		}
		entry := vendorstate.Entry{ImportPath: s.RootPath, GoVersion: s.Env.GOVERSION, GOOS: s.Env.GOOS, GOARCH: s.Env.GOARCH}
		for _, rule := range s.Statuses {
			entry.Rules = append(entry.Rules, vendorstate.Selection{ID: rule.Rule, Target: rule.Target, Version: rule.Version, State: rule.State})
		}
		plan.Entries = append(plan.Entries, entry)
		for pkg, p := range s.Packages {
			if p.Standard || p.Module == nil || p.Module.Main || strings.Contains(pkg, " [") {
				continue
			}
			if _, original := s.Packages[pkg]; !original {
				continue
			}
			effective := map[string][]byte{}
			sources, e := Sources(p, s.Overlay, s.Names())
			if e != nil {
				return e
			}
			for _, src := range sources {
				effective[filepath.Base(src.Path)] = src.Data
			}
			if r, ok := changes[pkg]; ok {
				for _, src := range sources {
					dest := filepath.Join(vendorRoot, filepath.FromSlash(pkg), filepath.Base(src.Path))
					plan.Inputs[dest] = vendorstate.Input{Exists: true, Hash: hash(src.Data)}
				}
				var owners []string
				seen := map[string]bool{}
				for _, match := range r.Result.Matches {
					if !seen[match.Rule] {
						owners = append(owners, match.Rule)
						seen[match.Rule] = true
					}
				}
				sort.Strings(owners)
				for name, b := range r.Result.Replacements {
					base := filepath.Base(name)
					effective[base] = b
					dest := filepath.Join(vendorRoot, filepath.FromSlash(pkg), base)
					outputs[dest] = portableLines(b, dest, pkg, s)
					plan.Owners[dest] = owners
				}
				for name, b := range r.Result.Additions {
					base := filepath.Base(name)
					effective[base] = b
					dest := filepath.Join(vendorRoot, filepath.FromSlash(pkg), base)
					physical := dest
					if !exists {
						physical = filepath.Join(stage, filepath.FromSlash(pkg), base)
					}
					if _, e := os.Stat(physical); e == nil {
						if replacement, managed := s.Overlay[dest]; !managed || replacement != "" {
							return fmt.Errorf("generated vendor file already occupied: %s", dest)
						}
					}
					outputs[dest] = portableLines(b, dest, pkg, s)
					plan.Owners[dest] = owners
					plan.Inputs[dest] = vendorstate.Input{}
				}
			}
			if prev, ok := shared[pkg]; ok {
				if !equalFiles(prev, effective) {
					return fmt.Errorf("vendor conflict: entries require different source for package %s (entry %s)", pkg, s.RootPath)
				}
			} else {
				shared[pkg] = effective
			}
		}
	}
	sort.Slice(plan.Entries, func(i, j int) bool { return plan.Entries[i].ImportPath < plan.Entries[j].ImportPath })
	portable := map[string][]byte{}
	for name, data := range outputs {
		rel, e := filepath.Rel(vendorRoot, name)
		if e != nil {
			return e
		}
		portable[filepath.ToSlash(rel)] = data
	}
	data, _ := json.Marshal(struct {
		Entries []vendorstate.Entry
		Files   map[string][]byte
	}{plan.Entries, portable})
	plan.Fingerprint = hash(data)
	if !exists {
		return vendorstate.Install(vendorRoot, stage, outputs, plan)
	}
	return vendorstate.ApplyPlan(vendorRoot, outputs, plan)
}

func equalFiles(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !bytes.Equal(v, b[k]) {
			return false
		}
	}
	return true
}
