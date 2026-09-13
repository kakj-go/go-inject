package build

import (
	"os"
	"path/filepath"
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
		if strings.HasSuffix(arg, ".go") {
			logical := arg
			if !filepath.IsAbs(logical) {
				logical = filepath.Join(cwd, logical)
			}
			if replacement, ok := s.Overlay[filepath.Clean(logical)]; ok {
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
