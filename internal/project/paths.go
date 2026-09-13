package project

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Go compares workspace module directories lexically. Canonicalizing only cwd
// is insufficient: an inherited /var/.../go.work on macOS would still expand
// its relative use directives underneath /var while cwd is /private/var.
func canonicalDirectory(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	if canonical, err := filepath.EvalSymlinks(abs); err == nil {
		return canonical
	}
	return abs
}

func canonicalWorkfile(name string) string {
	if name == "" || name == "off" || !filepath.IsAbs(name) {
		return name
	}
	if canonical, err := filepath.EvalSymlinks(name); err == nil {
		return canonical
	}
	return name
}

func commandEnvironment(dir string, inherited []string) []string {
	result := make([]string, 0, len(inherited)+1)
	for _, item := range inherited {
		key, value, ok := strings.Cut(item, "=")
		if ok && environmentKeyEqual(key, "GOWORK") {
			item = key + "=" + canonicalWorkfile(value)
		}
		if ok && environmentKeyEqual(key, "PWD") {
			continue
		}
		result = append(result, item)
	}
	// exec.Cmd only synthesizes PWD when Env is nil. Keep it consistent when
	// providing our normalized environment explicitly.
	return append(result, "PWD="+dir)
}

func environmentKeyEqual(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
