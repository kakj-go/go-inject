package build

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNativeBaselineResolvesFileIdentity(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.go")
	baseline := filepath.Join(dir, "baseline.go")
	if err := os.WriteFile(source, []byte("package p\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Session{Overlay: map[string]string{source: baseline}}
	alias := source
	if runtime.GOOS == "windows" {
		alias = strings.ToUpper(source)
	} else {
		linked := filepath.Join(dir, "alias.go")
		if err := os.Symlink(source, linked); err != nil {
			t.Fatal(err)
		}
		alias = linked
	}
	got, err := s.nativeBaselineArgs([]string{"compile", alias}, "compile")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1] != baseline {
		t.Fatalf("baseline alias not resolved: %v", got)
	}
	s.Overlay[source] = ""
	got, err = s.nativeBaselineArgs([]string{"compile", alias}, "compile")
	if err != nil || len(got) != 1 {
		t.Fatalf("generated alias not hidden: %v %v", got, err)
	}
}
