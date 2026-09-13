// Package project resolves build inputs through the Go command.
package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/build"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/kakj-go/go-inject/internal/process"
)

type Module struct {
	Path, Version, Dir, GoMod string
	Main                      bool
	Replace                   *Module
}

type Package struct {
	ImportPath, Name, Dir, ForTest, Export                                                                          string
	Standard                                                                                                        bool
	GoFiles, CgoFiles, IgnoredGoFiles, TestGoFiles, XTestGoFiles, Imports, Deps, CFiles, HFiles, SFiles, EmbedFiles []string
	Module                                                                                                          *Module
	Error                                                                                                           *struct{ Err string }
	DepsErrors                                                                                                      []struct{ Err string }
	ImportMap                                                                                                       map[string]string
	EntryFiles                                                                                                      []string
}

func (p *Package) Base() string { return strings.Split(p.ImportPath, " [")[0] }
func (p *Package) Version() string {
	if p.Module == nil {
		return ""
	}
	if p.Module.Replace != nil {
		return p.Module.Replace.Version
	}
	return p.Module.Version
}

type Env struct {
	Dir, Go, GOOS, GOARCH, GOROOT, GOPATH, GOMODCACHE, GOMOD, GOWORK, GOVERSION, CGO_ENABLED string
	GOFLAGS, GOEXPERIMENT, Compiler                                                          string
	ReleaseTags, ToolTags                                                                    []string
}

func Command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, "go", args...)
	c.Dir = canonicalDirectory(dir)
	c.Env = commandEnvironment(c.Dir, os.Environ())
	return c
}

func Environment(ctx context.Context, dir string) (Env, error) {
	var e Env
	command := Command(ctx, dir, "env", "-json", "GOOS", "GOARCH", "GOROOT", "GOPATH", "GOMODCACHE", "GOMOD", "GOWORK", "GOVERSION", "CGO_ENABLED", "GOFLAGS", "GOEXPERIMENT")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := process.Run(ctx, command)
	if err != nil {
		return e, fmt.Errorf("go env: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	if err = json.Unmarshal(stdout.Bytes(), &e); err != nil {
		return e, err
	}
	e.Dir = command.Dir
	e.GOWORK = canonicalWorkfile(e.GOWORK)
	e.Go, err = exec.LookPath("go")
	if e.GOMOD == "" || e.GOMOD == os.DevNull {
		if e.GOWORK == "" || e.GOWORK == "off" {
			return e, fmt.Errorf("go-inject requires a Go module or workspace")
		}
	}
	if !strings.HasPrefix(e.GOVERSION, "go1.26.") && !strings.HasPrefix(e.GOVERSION, "go1.27.") {
		return e, fmt.Errorf("unsupported Go toolchain %s; supported series: Go 1.26 and 1.27", e.GOVERSION)
	}
	if err != nil {
		return e, err
	}
	if err := e.loadToolContext(ctx); err != nil {
		return e, err
	}
	return e, nil
}

func Explain(err error) error {
	var e *exec.ExitError
	if errors.As(err, &e) && len(e.Stderr) > 0 {
		return fmt.Errorf("%s: %w", bytes.TrimSpace(e.Stderr), err)
	}
	return err
}

func List(ctx context.Context, dir string, flags []string, deps, tests bool, patterns ...string) ([]*Package, error) {
	for _, pattern := range patterns {
		if pattern == "" || strings.HasPrefix(pattern, "-") || strings.ContainsAny(pattern, "\x00\r\n") {
			return nil, fmt.Errorf("invalid Go package pattern %q", pattern)
		}
	}
	a := []string{"list", "-e", "-json"}
	if deps {
		a = append(a, "-deps")
	}
	if tests {
		a = append(a, "-test")
	}
	a = append(a, flags...)
	a = append(a, "--")
	a = append(a, patterns...)
	c := Command(ctx, dir, a...)
	var out, stderr bytes.Buffer
	c.Stdout = &out
	c.Stderr = &stderr
	if err := process.Run(ctx, c); err != nil {
		return nil, fmt.Errorf("go list: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	dec := json.NewDecoder(&out)
	var result []*Package
	for {
		var p Package
		err := dec.Decode(&p)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		result = append(result, &p)
	}
	return result, nil
}

func Index(pkgs []*Package) map[string]*Package {
	m := make(map[string]*Package)
	for _, p := range pkgs {
		key := p.Base()
		if old := m[key]; old == nil || (old.ForTest != "" && p.ForTest == "") {
			m[key] = p
		}
	}
	return m
}

func (e Env) Context(flags []string, registration bool) build.Context {
	c := build.Default
	c.GOOS = e.GOOS
	c.GOARCH = e.GOARCH
	c.GOROOT = e.GOROOT
	c.GOPATH = e.GOPATH
	c.CgoEnabled = e.CGO_ENABLED == "1"
	c.ReleaseTags = append([]string(nil), e.ReleaseTags...)
	v := strings.TrimPrefix(e.GOVERSION, "go1.")
	minor, _ := strconv.Atoi(strings.Split(v, ".")[0])
	if len(c.ReleaseTags) == 0 {
		for i := 1; i <= minor; i++ {
			c.ReleaseTags = append(c.ReleaseTags, fmt.Sprintf("go1.%d", i))
		}
	}
	c.ToolTags = append([]string(nil), e.ToolTags...)
	allFlags := append(inheritedFlags(e.GOFLAGS), flags...)
	c.BuildTags = Tags(allFlags)
	if e.Compiler != "" {
		c.Compiler = e.Compiler
	}
	if compiler, ok := Value(allFlags, "compiler"); ok {
		c.Compiler = compiler
	}
	for _, mode := range []string{"race", "msan", "asan"} {
		if Has(allFlags, mode) {
			c.ToolTags = append(c.ToolTags, mode)
		}
	}
	if registration {
		c.BuildTags = append(c.BuildTags, "goinject")
	}
	return c
}

// Query the selected Go tool, not the Go version/architecture which built
// go-inject. ToolTags include default and explicitly changed experiments plus
// target architecture feature levels (GOAMD64, GOARM64, and so on).
func (e *Env) loadToolContext(ctx context.Context) error {
	const format = `{{ $c := context }}{"releaseTags":{{printf "%q" (join $c.ReleaseTags ",")}},"toolTags":{{printf "%q" (join $c.ToolTags ",")}},"compiler":{{printf "%q" $c.Compiler}}}`
	c := Command(ctx, e.Dir, "list", "-e", "-f", format, "--", "unsafe")
	// A nonempty whitespace value suppresses persistent GOFLAGS without
	// discarding GOENV's target/experiment settings. Flags are applied by Context.
	c.Env = append(c.Environ(), "GOFLAGS= ")
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	if err := process.Run(ctx, c); err != nil {
		return fmt.Errorf("query Go build context: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	var result struct{ ReleaseTags, ToolTags, Compiler string }
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return fmt.Errorf("parse Go build context: %w", err)
	}
	e.ReleaseTags = strings.Split(result.ReleaseTags, ",")
	if result.ToolTags != "" {
		e.ToolTags = strings.Split(result.ToolTags, ",")
	}
	e.Compiler = result.Compiler
	return nil
}

func Read(path string, overlay map[string]string) ([]byte, error) {
	if replacement, ok := overlay[path]; ok {
		if replacement == "" {
			return nil, os.ErrNotExist
		}
		path = replacement
	}
	return os.ReadFile(path)
}

func OverlayFile(path string, mappings map[string]string) error {
	data, err := json.Marshal(struct{ Replace map[string]string }{mappings})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
