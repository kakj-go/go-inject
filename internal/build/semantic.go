package build

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/kakj-go/go-inject/internal/project"
	"github.com/kakj-go/go-inject/internal/rewrite"
	"github.com/kakj-go/go-inject/internal/rules"
	"github.com/kakj-go/go-inject/internal/semantic"
)

func (s *Session) targetSources(target string) ([]rewrite.Source, string, error) {
	name := target
	if target == "main" {
		name = s.RootPath
		if s.Kind == "test" {
			return []rewrite.Source{{Path: filepath.Join(s.Root.Dir, "_testmain.go"), Data: []byte("package main\nfunc main(){}\n"), ImportNames: s.Names()}}, name, nil
		}
		src, err := Sources(s.Root, s.Overlay, s.Names())
		return src, name, err
	}
	pkg := s.Packages[target]
	if pkg == nil {
		return nil, name, fmt.Errorf("target package %s is not available", target)
	}
	src, err := Sources(pkg, s.Overlay, s.Names())
	return src, name, err
}

func (s *Session) bindRules(ctx context.Context, flags []string) error {
	if len(s.Rules) == 0 {
		return nil
	}
	if err := s.loadBridgePackages(ctx, flags); err != nil {
		return err
	}
	targets := map[string]bool{}
	for _, rule := range s.Rules {
		targets[rules.TargetPackage(rule.Target)] = true
	}
	overlays := map[string]string{}
	for k, v := range s.Overlay {
		overlays[k] = v
	}
	staged := map[string][]rewrite.Source{}
	for target := range targets {
		sources, name, err := s.targetSources(target)
		if err != nil {
			return err
		}
		result, err := rewrite.Declarations(name, sources, s.RulesFor(target))
		if err != nil {
			return fmt.Errorf("target %s version %q: %w", target, packageVersion(s.Packages[target]), err)
		}
		for i, src := range sources {
			if data, ok := result.Replacements[src.Path]; ok {
				sources[i].Data = data
			}
		}
		for path, data := range result.Replacements {
			if err = s.stage(overlays, path, data, "declarations"); err != nil {
				return err
			}
		}
		dir := s.Root.Dir
		if target != "main" {
			dir = s.Packages[target].Dir
		}
		for path, data := range result.Additions {
			main := strings.HasPrefix(filepath.ToSlash(path), "main/")
			if main && s.Kind == "test" {
				continue
			}
			destDir := dir
			if main {
				destDir = s.Root.Dir
			}
			logical := filepath.Join(destDir, filepath.Base(path))
			if _, err := project.Read(logical, s.Overlay); err == nil {
				return fmt.Errorf("generated file already occupied: %s", logical)
			} else if !os.IsNotExist(err) {
				return err
			}
			if err = s.stage(overlays, logical, data, "declarations"); err != nil {
				return err
			}
			if !main || target == "main" {
				sources = append(sources, rewrite.Source{Path: logical, Data: data, ImportNames: s.Names()})
			}
		}
		staged[target] = sources
	}
	overlayPath := filepath.Join(s.Dir, "declarations.json")
	if err := project.OverlayFile(overlayPath, overlays); err != nil {
		return err
	}
	flags = project.Remove(flags, "overlay", true)
	contents := map[string][]byte{}
	for logical, backing := range overlays {
		if backing != "" {
			data, e := os.ReadFile(backing)
			if e != nil {
				return e
			}
			contents[logical] = data
			continue
		}
		// go/packages overlays cannot encode deletions. Tool-owned deleted Go
		// files are represented as excluded files in the semantic source view.
		if !strings.HasSuffix(logical, ".go") {
			return fmt.Errorf("semantic view cannot delete non-Go overlay file %s", logical)
		}
		file, e := parser.ParseFile(token.NewFileSet(), logical, nil, parser.PackageClauseOnly)
		if e != nil {
			return e
		}
		contents[logical] = []byte("//go:build ignore\n\npackage " + file.Name.Name + "\n")
	}
	entries := append([]string{}, s.EntryArgs...)
	if s.RootPath == "command-line-arguments" {
		seen := map[string]bool{}
		for _, name := range entries {
			seen[name] = true
		}
		for _, src := range staged["main"] {
			if !seen[src.Path] && s.Kind != "test" {
				entries = append(entries, src.Path)
				seen[src.Path] = true
			}
		}
	}
	model := semantic.New(ctx, s.Env.Dir, flags, contents, s.RootPath, s.Kind == "test", entries...)
	environment, typeFlags, err := s.typeEnvironment(flags)
	if err != nil {
		return err
	}
	model.Environment = environment
	model.SetFlags(typeFlags)
	updated := map[string]rewrite.Rule{}
	for target := range targets {
		path := target
		if target == "main" {
			path = s.RootPath
			if s.Kind == "test" {
				path += ".test"
			}
		}
		bound, err := model.Bind(path, staged[target], s.RulesFor(target))
		if err != nil {
			return fmt.Errorf("target %s version %q: %w", target, packageVersion(s.Packages[target]), err)
		}
		for _, rule := range bound {
			updated[rule.ID] = rule
		}
	}
	for i, rule := range s.Rules {
		s.Rules[i] = updated[rule.ID]
	}
	return nil
}

func (s *Session) stage(overlays map[string]string, path string, data []byte, kind string) error {
	dir := filepath.Join(s.Dir, kind)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	backing := filepath.Join(dir, hash([]byte(path))+".go")
	if err := os.WriteFile(backing, data, 0600); err != nil {
		return err
	}
	overlays[path] = backing
	return nil
}

// ValidateGenerated compiles the complete planned Go view without executing it
// or committing vendor changes. This also checks added declarations and imports.
func (s *Session) ValidateGenerated(ctx context.Context) error {
	overlays := map[string]string{}
	for k, v := range s.Overlay {
		overlays[k] = v
	}
	results, err := s.PlannedResults()
	if err != nil {
		return err
	}
	for pkg, result := range results {
		p := s.Packages[pkg]
		if p == nil {
			continue
		}
		for path, data := range result.Replacements {
			if err = s.stage(overlays, path, data, "validation"); err != nil {
				return err
			}
		}
		for name, data := range result.Additions {
			if strings.HasPrefix(filepath.ToSlash(name), "main/") {
				return fmt.Errorf("vendor does not support main additions")
			}
			if err = s.stage(overlays, filepath.Join(p.Dir, filepath.Base(name)), data, "validation"); err != nil {
				return err
			}
		}
	}
	path := filepath.Join(s.Dir, "validation.json")
	if err = project.OverlayFile(path, overlays); err != nil {
		return err
	}
	flags := append(project.Remove(project.ListFlags(s.Flags), "overlay", true), "-overlay", path, "-export")
	pkgs, err := project.List(ctx, s.Env.Dir, flags, true, false, s.EntryArgs...)
	if err != nil {
		return err
	}
	for _, p := range pkgs {
		if p.Error != nil {
			return fmt.Errorf("generated package %s: %s", p.Base(), p.Error.Err)
		}
		if len(p.DepsErrors) > 0 {
			return fmt.Errorf("generated package %s: %s", p.Base(), p.DepsErrors[0].Err)
		}
	}
	return nil
}
