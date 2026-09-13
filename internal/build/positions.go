package build

import (
	"go/scanner"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

// Only actual line-directive comment tokens are rewritten. Vendor output is
// portable between checkouts and does not retain a developer's cache/home path.
func portableLines(data []byte, dest, pkg string, s *Session) []byte {
	names := map[string]string{}
	if p := s.Packages[pkg]; p != nil {
		for _, file := range append(append([]string{}, p.GoFiles...), p.CgoFiles...) {
			names[filepath.ToSlash(filepath.Join(p.Dir, file))] = filepath.Join(VendorRoot(s.Env), filepath.FromSlash(pkg), filepath.Base(file))
		}
	}
	for _, rule := range s.Rules {
		logical := rule.Path
		if strings.HasPrefix(filepath.ToSlash(rule.Path), filepath.ToSlash(s.Env.GOMODCACHE)+"/") {
			logical = filepath.Join(VendorRoot(s.Env), filepath.FromSlash(rule.Provider), filepath.Base(rule.Path))
		}
		names[filepath.ToSlash(rule.Path)] = logical
	}
	fset := token.NewFileSet()
	file := fset.AddFile(dest, -1, len(data))
	var scan scanner.Scanner
	scan.Init(file, data, nil, scanner.ScanComments)
	type edit struct {
		start, end int
		value      string
	}
	var edits []edit
	for {
		pos, kind, value := scan.Scan()
		if kind == token.EOF {
			break
		}
		if kind != token.COMMENT || !strings.HasPrefix(value, "//line ") {
			continue
		}
		text := strings.TrimPrefix(value, "//line ")
		cut := strings.LastIndexByte(text, ':')
		if cut < 0 {
			continue
		}
		if _, err := strconv.Atoi(text[cut+1:]); err != nil {
			continue
		}
		path, suffix := text[:cut], text[cut:]
		if last := strings.LastIndexByte(path, ':'); last >= 0 {
			if _, err := strconv.Atoi(path[last+1:]); err == nil {
				suffix = path[last:] + suffix
				path = path[:last]
			}
		}
		logical, ok := names[path]
		if !ok {
			continue
		}
		rel, err := filepath.Rel(filepath.Dir(dest), logical)
		if err != nil {
			continue
		}
		offset := file.Offset(pos)
		edits = append(edits, edit{offset, offset + len(value), "//line " + filepath.ToSlash(rel) + suffix})
	}
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		next := make([]byte, 0, len(data)+len(e.value))
		next = append(next, data[:e.start]...)
		next = append(next, e.value...)
		next = append(next, data[e.end:]...)
		data = next
	}
	return data
}
