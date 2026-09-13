package vendorstate

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newVendor(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "vendor")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func put(t *testing.T, name, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func wantFile(t *testing.T, name, want string) {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", name, data, want)
	}
}

func wantAbsent(t *testing.T, name string) {
	t.Helper()
	if _, err := os.Lstat(name); !os.IsNotExist(err) {
		t.Fatalf("%s should not exist: %v", name, err)
	}
}

func TestApplySwitchOverlayRestore(t *testing.T) {
	root := newVendor(t)
	target := filepath.Join(root, "example.com", "library", "source.go")
	generated := filepath.Join(root, "example.com", "library", "generated.go")
	other := filepath.Join(root, "unrelated.txt")
	put(t, target, "original source with user's patch\n")
	put(t, other, "unrelated first version")
	first := map[string][]byte{target: []byte("plan one\n"), generated: []byte("new helper\n")}
	if err := Apply(root, first, "plan-one"); err != nil {
		t.Fatal(err)
	}
	if !HasState(root) {
		t.Fatal("missing state")
	}
	wantFile(t, target, "plan one\n")
	wantFile(t, generated, "new helper\n")
	if err := Apply(root, first, "plan-one"); err != nil {
		t.Fatal(err)
	}
	overlay, err := Overlay(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wantFile(t, overlay[target], "original source with user's patch\n")
	if backing, ok := overlay[generated]; !ok || backing != "" {
		t.Fatalf("generated file not hidden: %#v", overlay)
	}
	put(t, other, "unrelated user update")
	second := map[string][]byte{target: []byte("plan two\n")}
	if err := Apply(root, second, "plan-two"); err != nil {
		t.Fatal(err)
	}
	wantFile(t, target, "plan two\n")
	wantAbsent(t, generated)
	wantFile(t, other, "unrelated user update")
	// An overlay remains an independent snapshot after another plan is applied.
	wantFile(t, overlay[target], "original source with user's patch\n")
	if err := Restore(root); err != nil {
		t.Fatal(err)
	}
	wantFile(t, target, "original source with user's patch\n")
	wantFile(t, other, "unrelated user update")
	if HasState(root) {
		t.Fatal("state should be absent after restore")
	}
	if err := Restore(root); err != nil {
		t.Fatal(err)
	}
}

func TestManualRestoreToBaselineIsAccepted(t *testing.T) {
	root := newVendor(t)
	target := filepath.Join(root, "file.go")
	put(t, target, "base")
	if err := Apply(root, map[string][]byte{target: []byte("first")}, "one"); err != nil {
		t.Fatal(err)
	}
	put(t, target, "base")
	if err := Apply(root, map[string][]byte{target: []byte("second")}, "two"); err != nil {
		t.Fatal(err)
	}
	if err := Restore(root); err != nil {
		t.Fatal(err)
	}
	wantFile(t, target, "base")
}

func TestReadOnlyBaselineRoundTrip(t *testing.T) {
	root := newVendor(t)
	target := filepath.Join(root, "source.go")
	put(t, target, "read-only baseline")
	if err := os.Chmod(target, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(target, 0o644) })
	if err := Apply(root, map[string][]byte{target: []byte("read-only generated")}, "one"); err != nil {
		t.Fatal(err)
	}
	wantFile(t, target, "read-only generated")
	if err := Restore(root); err != nil {
		t.Fatal(err)
	}
	wantFile(t, target, "read-only baseline")
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("read-only mode lost: %v", info.Mode())
	}
}

func TestUserChangesAreNotOverwritten(t *testing.T) {
	for _, kind := range []string{"original", "generated"} {
		t.Run(kind, func(t *testing.T) {
			root := newVendor(t)
			target := filepath.Join(root, "file.go")
			other := filepath.Join(root, "other.go")
			if kind == "original" {
				put(t, target, "base")
			}
			put(t, other, "unchanged")
			if err := Apply(root, map[string][]byte{target: []byte("generated")}, "one"); err != nil {
				t.Fatal(err)
			}
			put(t, target, "user change after generation")
			if err := Apply(root, map[string][]byte{other: []byte("should never be written")}, "two"); !errors.Is(err, ErrConflict) {
				t.Fatalf("Apply error = %v, want conflict", err)
			}
			if err := Restore(root); !errors.Is(err, ErrConflict) {
				t.Fatalf("Restore error = %v, want conflict", err)
			}
			if _, err := Overlay(root, t.TempDir()); !errors.Is(err, ErrConflict) {
				t.Fatalf("Overlay error = %v, want conflict", err)
			}
			wantFile(t, target, "user change after generation")
			wantFile(t, other, "unchanged")
		})
	}
}

func TestMissingOriginalIsConflict(t *testing.T) {
	root := newVendor(t)
	target := filepath.Join(root, "file.go")
	put(t, target, "base")
	if err := Apply(root, map[string][]byte{target: []byte("after")}, "one"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := Restore(root); !errors.Is(err, ErrConflict) {
		t.Fatalf("Restore = %v, want conflict", err)
	}
	wantAbsent(t, target)
}

func TestApplyBaselineForgetsOwnership(t *testing.T) {
	root := newVendor(t)
	target := filepath.Join(root, "file.go")
	put(t, target, "base")
	if err := Apply(root, map[string][]byte{target: []byte("base")}, "same"); err != nil {
		t.Fatal(err)
	}
	if HasState(root) {
		t.Fatal("unchanged output should not acquire ownership")
	}
	if err := Apply(root, map[string][]byte{target: []byte("after")}, "change"); err != nil {
		t.Fatal(err)
	}
	if err := Apply(root, map[string][]byte{target: []byte("base")}, "back"); err != nil {
		t.Fatal(err)
	}
	if HasState(root) {
		t.Fatal("output identical to baseline should release ownership")
	}
}

func TestPathsAreContained(t *testing.T) {
	root := newVendor(t)
	outside := filepath.Join(filepath.Dir(root), "outside.go")
	put(t, outside, "outside")
	for _, path := range []string{outside, root, "relative.go"} {
		if err := Apply(root, map[string][]byte{path: []byte("bad")}, "bad"); err == nil {
			t.Fatalf("accepted path %q", path)
		}
	}
	wantFile(t, outside, "outside")
	occupied := filepath.Join(root, "directory.go")
	if err := os.Mkdir(occupied, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Apply(root, map[string][]byte{occupied: []byte("bad")}, "bad"); err == nil {
		t.Fatal("accepted an output occupied by a directory")
	}
}

func TestSymlinkTargetsAreRejected(t *testing.T) {
	root := newVendor(t)
	outside := t.TempDir()
	put(t, filepath.Join(outside, "source.go"), "outside")
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := Apply(root, map[string][]byte{filepath.Join(link, "source.go"): []byte("bad")}, "bad"); err == nil {
		t.Fatal("accepted symlink parent")
	}
	wantFile(t, filepath.Join(outside, "source.go"), "outside")
	fileLink := filepath.Join(root, "source.go")
	if err := os.Symlink(filepath.Join(outside, "source.go"), fileLink); err != nil {
		t.Fatal(err)
	}
	if err := Apply(root, map[string][]byte{fileLink: []byte("bad")}, "bad"); err == nil {
		t.Fatal("accepted symlink file")
	}
}

func TestStateSymlinkIsRejected(t *testing.T) {
	root := newVendor(t)
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "vendor-state"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(root), ".goinject")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := Apply(root, nil, "bad"); err == nil {
		t.Fatal("accepted a symlink state parent")
	}
	wantAbsent(t, filepath.Join(outside, "vendor-state", "lock"))
}

func TestWindowsAliases(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path aliases")
	}
	root := newVendor(t)
	target := filepath.Join(root, "Source.go")
	put(t, target, "base")
	if err := Apply(root, map[string][]byte{target: []byte("one")}, "one"); err != nil {
		t.Fatal(err)
	}
	if err := Apply(root, map[string][]byte{strings.ToUpper(target): []byte("two")}, "two"); err != nil {
		t.Fatal(err)
	}
	if err := Restore(root); err != nil {
		t.Fatal(err)
	}
	wantFile(t, target, "base")
	for _, suffix := range []string{"file.go:stream", "file.go.", "file.go ", "CON", "NUL.txt"} {
		if err := Apply(root, map[string][]byte{filepath.Join(root, suffix): []byte("bad")}, "bad"); err == nil {
			t.Fatalf("accepted Windows alias %q", suffix)
		}
	}
}

func TestEmptyRootAndNoState(t *testing.T) {
	root := filepath.Join(t.TempDir(), "vendor")
	if HasState(root) {
		t.Fatal("unexpected state")
	}
	overlay, err := Overlay(root, "")
	if err != nil || len(overlay) != 0 {
		t.Fatalf("Overlay = %v, %v", overlay, err)
	}
	target := filepath.Join(root, "package", "new.go")
	if err := Apply(root, map[string][]byte{target: []byte("new")}, "new"); err != nil {
		t.Fatal(err)
	}
	wantFile(t, target, "new")
	if err := Restore(root); err != nil {
		t.Fatal(err)
	}
	wantAbsent(t, target)
}

func TestOverlayRefusesVendorDestination(t *testing.T) {
	root := newVendor(t)
	target := filepath.Join(root, "file.go")
	put(t, target, "base")
	if err := Apply(root, map[string][]byte{target: []byte("after")}, "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := Overlay(root, filepath.Join(root, "overlays")); err == nil {
		t.Fatal("accepted overlay output within vendor")
	}
}
