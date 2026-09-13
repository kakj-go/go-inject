package vendorstate

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func pendingChange(t *testing.T, w *workspace, outputs map[string][]byte) *journal {
	t.Helper()
	old, raw, err := w.readManifest()
	if err != nil {
		t.Fatal(err)
	}
	next := &manifest{Version: stateVersion, Root: ".", Fingerprint: "interrupted", Files: make(map[string]record)}
	if old != nil {
		for rel, entry := range old.Files {
			next.Files[rel] = entry
		}
	}
	j := &journal{Version: stateVersion, Root: w.root, BeforeManifest: raw}
	for _, rel := range sortedKeys(outputs) {
		before, err := w.readSource(rel)
		if err != nil {
			t.Fatal(err)
		}
		baseline := before
		if old != nil {
			if entry, ok := old.Files[rel]; ok {
				baseline = entry.Baseline
			}
		}
		next.Files[rel] = record{Baseline: baseline, OutputHash: contentHash(outputs[rel])}
		mode := before.Mode
		if !before.Exists {
			mode = 0o644
		}
		j.Changes = append(j.Changes, fileChange{Path: rel, Before: before, After: diskFile{Exists: true, Data: outputs[rel], Mode: mode}})
	}
	j.AfterManifest, err = encodeManifest(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.prepareJournal(j); err != nil {
		t.Fatal(err)
	}
	return j
}

func TestRecoverEveryCommitStage(t *testing.T) {
	for _, stage := range []string{"journal", "first-file", "manifest"} {
		t.Run(stage, func(t *testing.T) {
			root := newVendor(t)
			a := filepath.Join(root, "a.go")
			b := filepath.Join(root, "b.go")
			put(t, a, "original-a")
			put(t, b, "original-b")
			if err := Apply(root, map[string][]byte{a: []byte("previous-a"), b: []byte("previous-b")}, "previous"); err != nil {
				t.Fatal(err)
			}
			w, unlock, err := openLocked(root)
			if err != nil {
				t.Fatal(err)
			}
			j := pendingChange(t, w, map[string][]byte{"a.go": []byte("next-a"), "b.go": []byte("next-b")})
			if stage == "first-file" {
				if err := writeAtomic(a, j.Changes[0].After); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "manifest" {
				if err := w.applyJournal(j); err != nil {
					t.Fatal(err)
				}
			}
			unlock()
			overlay, err := Overlay(root, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			wantFile(t, a, "previous-a")
			wantFile(t, b, "previous-b")
			wantFile(t, overlay[a], "original-a")
			wantFile(t, overlay[b], "original-b")
			wantAbsent(t, w.journalPath())
			if err := Restore(root); err != nil {
				t.Fatal(err)
			}
			wantFile(t, a, "original-a")
			wantFile(t, b, "original-b")
		})
	}
}

func TestRecoveryPreservesChangesMadeAfterInterruption(t *testing.T) {
	root := newVendor(t)
	a := filepath.Join(root, "a.go")
	b := filepath.Join(root, "b.go")
	put(t, a, "original-a")
	put(t, b, "original-b")
	w, unlock, err := openLocked(root)
	if err != nil {
		t.Fatal(err)
	}
	j := pendingChange(t, w, map[string][]byte{"a.go": []byte("partial-a"), "b.go": []byte("partial-b")})
	if err := writeAtomic(a, j.Changes[0].After); err != nil {
		t.Fatal(err)
	}
	unlock()
	put(t, b, "user change after interruption")
	if _, err := Overlay(root, t.TempDir()); !errors.Is(err, ErrConflict) {
		t.Fatalf("Overlay error = %v, want conflict", err)
	}
	wantFile(t, a, "partial-a") // All paths are checked before any rollback.
	wantFile(t, b, "user change after interruption")
	if !HasState(root) {
		t.Fatal("recovery journal was lost")
	}
	put(t, b, "original-b")
	if err := Restore(root); err != nil {
		t.Fatal(err)
	}
	wantFile(t, a, "original-a")
	wantFile(t, b, "original-b")
	if HasState(root) {
		t.Fatal("completed recovery left active state")
	}
}

func TestRecoveryRemovesOnlyItsNewFilesAndEmptyDirectories(t *testing.T) {
	root := newVendor(t)
	w, unlock, err := openLocked(root)
	if err != nil {
		t.Fatal(err)
	}
	j := pendingChange(t, w, map[string][]byte{"new/deep/a.go": []byte("partial"), "empty/deep/b.go": []byte("partial")})
	if err := w.applyJournal(j); err != nil {
		t.Fatal(err)
	}
	unlock()
	put(t, filepath.Join(root, "new", "user.txt"), "user-owned")
	if err := Restore(root); err != nil {
		t.Fatal(err)
	}
	wantAbsent(t, filepath.Join(root, "new", "deep"))
	wantAbsent(t, filepath.Join(root, "empty"))
	wantFile(t, filepath.Join(root, "new", "user.txt"), "user-owned")
}

func TestCorruptManifestAndJournalAreNotTrusted(t *testing.T) {
	for _, filename := range []string{"manifest.json", "transaction.json"} {
		t.Run(filename, func(t *testing.T) {
			root := newVendor(t)
			target := filepath.Join(root, "file.go")
			put(t, target, "original")
			w, unlock, err := openLocked(root)
			if err != nil {
				t.Fatal(err)
			}
			put(t, filepath.Join(w.stateDir, filename), `{"version":1,"root":"wrong root"}`)
			unlock()
			if err := Apply(root, map[string][]byte{target: []byte("bad")}, "bad"); err == nil {
				t.Fatal("accepted corrupted state")
			}
			wantFile(t, target, "original")
		})
	}
}

func TestJournalRejectsEscapingPaths(t *testing.T) {
	root := newVendor(t)
	w, unlock, err := openLocked(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	j := &journal{Version: stateVersion, Root: w.root, Changes: []fileChange{{Path: "../outside.go"}}}
	if err := w.prepareJournal(j); err == nil {
		t.Fatal("accepted escaping journal change")
	}
	wantAbsent(t, w.journalPath())
}

// TestProcessHelper is only active in child invocations. It intentionally uses
// a separate process so locks and recovery are tested against OS process death,
// rather than only against mutexes in one Go process.
func TestProcessHelper(t *testing.T) {
	mode := os.Getenv("GOINJECT_VENDORSTATE_TEST_MODE")
	if mode == "" {
		return
	}
	root := os.Getenv("GOINJECT_VENDORSTATE_TEST_ROOT")
	w, unlock, err := openLocked(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if mode == "interruption" {
		j := pendingChange(t, w, map[string][]byte{"source.go": []byte("interrupted-output")})
		if err := writeAtomic(filepath.Join(w.root, "source.go"), j.Changes[0].After); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Fprintln(os.Stdout, "ready")
	var b [1]byte
	os.Stdin.Read(b[:])
}

func helperProcess(t *testing.T, root, mode string) (*exec.Cmd, io.WriteCloser) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestProcessHelper$", "-test.timeout=20s")
	cmd.Env = append(os.Environ(), "GOINJECT_VENDORSTATE_TEST_MODE="+mode, "GOINJECT_VENDORSTATE_TEST_ROOT="+root)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		cmd.Process.Kill()
	})
	ready := make(chan error, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		if err == nil && line != "ready\n" {
			err = fmt.Errorf("unexpected child output %q", line)
		}
		ready <- err
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not acquire vendor lock")
	}
	return cmd, stdin
}

func TestProcessLockSerializesMutations(t *testing.T) {
	root := newVendor(t)
	target := filepath.Join(root, "source.go")
	put(t, target, "original")
	cmd, stdin := helperProcess(t, root, "lock")
	done := make(chan error, 1)
	go func() { done <- Apply(root, map[string][]byte{target: []byte("generated")}, "after-lock") }()
	select {
	case err := <-done:
		t.Fatalf("Apply bypassed held process lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	wantFile(t, target, "original")
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Apply did not proceed after process released its lock")
	}
	wantFile(t, target, "generated")
}

func TestKilledProcessIsRecovered(t *testing.T) {
	root := newVendor(t)
	target := filepath.Join(root, "source.go")
	put(t, target, "original")
	cmd, _ := helperProcess(t, root, "interruption")
	wantFile(t, target, "interrupted-output")
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	if !HasState(root) {
		t.Fatal("missing pending transaction after process interruption")
	}
	overlay, err := Overlay(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(overlay) != 0 {
		t.Fatalf("initial transaction rollback should have no active baseline: %v", overlay)
	}
	wantFile(t, target, "original")
	if HasState(root) {
		t.Fatal("recovery left active state")
	}
}
