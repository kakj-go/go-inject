package build

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
		if e = c.Run(); e != nil {
			return e
		}
	}
	outputs := map[string][]byte{}
	owners := map[string]string{}
	var fingerprints []string
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
		fingerprints = append(fingerprints, s.Fingerprint)
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
				for name, b := range r.Result.Replacements {
					base := filepath.Base(name)
					effective[base] = b
					dest := filepath.Join(vendorRoot, filepath.FromSlash(pkg), base)
					outputs[dest] = b
					owners[dest] = s.RootPath
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
					outputs[dest] = b
					owners[dest] = s.RootPath
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
	_ = owners
	if !exists {
		if e = os.Rename(stage, vendorRoot); e != nil {
			if e = copyTree(stage, vendorRoot); e != nil {
				return e
			}
		}
	}
	sort.Strings(fingerprints)
	return vendorstate.Apply(vendorRoot, outputs, hash([]byte(strings.Join(fingerprints, "\n"))))
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
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(src, p)
		if e != nil {
			return e
		}
		to := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(to, 0755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink %s", p)
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		closeErr := out.Close()
		if e != nil {
			return e
		}
		return closeErr
	})
}
