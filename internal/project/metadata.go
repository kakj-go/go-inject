package project

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/tools/go/packages"
)

// ValidateImportPath accepts a literal package import, not a command flag,
// package pattern, local directory, file list, or module@version query.
func ValidateImportPath(name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasPrefix(name, ".") || strings.Contains(name, "...") || strings.ContainsAny(name, "@\\\x00\r\n") {
		return fmt.Errorf("invalid literal Go import path %q", name)
	}
	if err := module.CheckImportPath(name); err != nil {
		return fmt.Errorf("invalid Go import path %q: %w", name, err)
	}
	return nil
}

// Metadata locates an explicitly registered rule package through go/packages.
// It intentionally requests no import graph, export data, syntax, or types:
// unselected version implementations must not load their runtime imports.
// Package names, module identities, replace/workspace/vendor directories, and
// files are provided by the Go package loader rather than path-name guesses.
func Metadata(ctx context.Context, dir string, flags []string, name string) (*Package, error) {
	if err := ValidateImportPath(name); err != nil {
		return nil, err
	}
	command := Command(ctx, dir)
	loaded, err := packages.Load(&packages.Config{
		Context: ctx, Dir: command.Dir, Env: command.Environ(), BuildFlags: ListFlags(flags),
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedModule,
	}, name)
	if err != nil {
		return nil, fmt.Errorf("resolve rule package %q: %w", name, err)
	}
	if len(loaded) != 1 || loaded[0].Dir == "" || loaded[0].PkgPath != name {
		return nil, fmt.Errorf("cannot resolve literal rule package %q (run go mod tidy)", name)
	}
	p := loaded[0]
	for _, failure := range p.Errors {
		if len(p.GoFiles) == 0 && len(p.IgnoredFiles) > 0 && strings.Contains(failure.Msg, "build constraints exclude all Go files") {
			continue
		}
		return nil, fmt.Errorf("rule package %q: %s", name, failure)
	}
	result := &Package{ImportPath: p.PkgPath, Name: p.Name, Dir: p.Dir, Module: packageModule(p.Module)}
	for _, file := range p.GoFiles {
		result.GoFiles = append(result.GoFiles, filepath.Base(file))
	}
	for _, file := range p.IgnoredFiles {
		if strings.HasSuffix(file, ".go") {
			result.IgnoredGoFiles = append(result.IgnoredGoFiles, filepath.Base(file))
		}
	}
	return result, nil
}

func packageModule(m *packages.Module) *Module {
	if m == nil {
		return nil
	}
	return &Module{Path: m.Path, Version: m.Version, Dir: m.Dir, GoMod: m.GoMod, Main: m.Main, Replace: packageModule(m.Replace)}
}
