package build

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// cmd/go may discover an already injected vendor tree before the proxy starts.
// Both cgo and compile must receive baseline inputs, including hiding generated
// files, so the new selection never stacks onto an old injection.
func (s *Session) nativeBaselineArgs(args []string, kind string) ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(args))
	changed := false
	for _, arg := range args {
		if strings.HasSuffix(arg, ".go") || runtime.GOOS == "windows" && strings.HasSuffix(strings.ToLower(arg), ".go") {
			logical := arg
			if !filepath.IsAbs(logical) {
				logical = filepath.Join(cwd, logical)
			}
			if replacement, ok := s.baselineFor(logical); ok {
				changed = true
				if replacement == "" {
					continue
				}
				arg = replacement
			}
		}
		out = append(out, arg)
	}
	if changed && kind == "cgo" && option(out, "-srcdir") == "" {
		out = append([]string{out[0], "-srcdir", cwd}, out[1:]...)
	}
	return out, nil
}

func (s *Session) baselineFor(name string) (string, bool) {
	name = filepath.Clean(name)
	if replacement, ok := s.Overlay[name]; ok {
		return replacement, true
	}
	identity := sourceIdentity(name)
	for original, replacement := range s.Overlay {
		if sourceIdentity(original) == identity {
			return replacement, true
		}
	}
	return "", false
}

func sourceIdentity(name string) string {
	if abs, err := filepath.Abs(name); err == nil {
		name = abs
	}
	if resolved, err := filepath.EvalSymlinks(name); err == nil {
		name = resolved
	}
	if runtime.GOOS == "windows" {
		name = strings.ToLower(name)
	}
	return filepath.Clean(name)
}
