package vendorstate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func locate(root string) (*workspace, error) {
	logicalRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = absoluteDirectory(root)
	if err != nil {
		return nil, fmt.Errorf("vendor root: %w", err)
	}
	parent := filepath.Dir(root)
	if parent == root {
		return nil, fmt.Errorf("vendor root must not be a filesystem root: %q", root)
	}
	return &workspace{root: root, logicalRoot: filepath.Clean(logicalRoot), stateDir: filepath.Join(parent, ".goinject", "vendor-state")}, nil
}

// absoluteDirectory canonicalizes pre-existing ancestors (for example macOS
// /var) while refusing a symlink at the directory explicitly supplied by the
// caller. Paths below vendorRoot are checked separately and never followed.
func absoluteDirectory(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("directory path is empty")
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if info, err := os.Lstat(abs); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("refusing non-directory or symlink %q", abs)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return abs, nil
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		if os.IsNotExist(err) {
			resolvedParent, err = absoluteDirectory(parent)
		}
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(resolvedParent, filepath.Base(abs)), nil
}

func pathIdentity(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(name)
	}
	return name
}

func within(root, filename string) bool {
	rel, err := filepath.Rel(root, filename)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func validRelative(rel string) error {
	if rel == "" || rel == "." || strings.Contains(rel, "\\") || filepath.IsAbs(rel) {
		return fmt.Errorf("unsafe relative path %q", rel)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	if clean != rel || !filepath.IsLocal(filepath.FromSlash(rel)) || rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("unsafe relative path %q", rel)
	}
	if runtime.GOOS == "windows" {
		for _, segment := range strings.Split(rel, "/") {
			if strings.Contains(segment, ":") || strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") || windowsDeviceName(segment) {
				return fmt.Errorf("unsafe Windows path %q", rel)
			}
		}
	}
	return nil
}

func windowsDeviceName(segment string) bool {
	stem, _, _ := strings.Cut(strings.ToUpper(segment), ".")
	stem = strings.TrimRight(stem, " ")
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT") {
		suffix := strings.TrimPrefix(strings.TrimPrefix(stem, "COM"), "LPT")
		return len(suffix) == 1 && suffix[0] >= '1' && suffix[0] <= '9' || suffix == "¹" || suffix == "²" || suffix == "³"
	}
	return false
}

func (w *workspace) relative(filename string) (string, error) {
	if !filepath.IsAbs(filename) {
		return "", fmt.Errorf("vendor output must have an absolute path: %q", filename)
	}
	filename = filepath.Clean(filename)
	base := w.root
	if within(w.logicalRoot, filename) {
		base = w.logicalRoot
	} else if !within(w.root, filename) {
		return "", fmt.Errorf("vendor output escapes %q: %q", w.root, filename)
	}
	rel, err := filepath.Rel(base, filename)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if err := validRelative(rel); err != nil {
		return "", err
	}
	if err := w.checkSourcePath(rel); err != nil {
		return "", err
	}
	return rel, nil
}

func (w *workspace) checkSourcePath(rel string) error {
	if err := validRelative(rel); err != nil {
		return err
	}
	current := w.root
	if err := checkDirectory(current); err != nil {
		return err
	}
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		current = filepath.Join(current, part)
		if i == len(parts)-1 {
			return checkPlainFile(current)
		}
		if err := checkDirectory(current); err != nil {
			return err
		}
	}
	return nil
}

func (w *workspace) readSource(rel string) (diskFile, error) {
	if err := w.checkSourcePath(rel); err != nil {
		return diskFile{}, err
	}
	return readPlain(filepath.Join(w.root, filepath.FromSlash(rel)))
}

func checkDirectory(name string) error {
	info, err := os.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing non-directory or symlink %q", name)
	}
	return nil
}

func checkPlainFile(name string) error {
	info, err := os.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing non-regular file %q", name)
	}
	return nil
}

func mkdirPlain(name string, mode os.FileMode) error {
	if err := checkDirectory(name); err != nil {
		return err
	}
	if _, err := os.Lstat(name); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(name)
	if parent == name {
		return fmt.Errorf("cannot create filesystem root %q", name)
	}
	if err := mkdirPlain(parent, mode); err != nil {
		return err
	}
	if err := os.Mkdir(name, mode); err != nil && !os.IsExist(err) {
		return err
	}
	return checkDirectory(name)
}
