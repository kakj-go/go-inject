package vendorstate

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInitialDeliveryRecoveryStages(t *testing.T) {
	for _, point := range []string{"before-rename", "after-rename", "after-manifest"} {
		t.Run(point, func(t *testing.T) {
			base := t.TempDir()
			root := filepath.Join(base, "vendor")
			stage, err := os.MkdirTemp(base, ".goinject-vendor-")
			if err != nil {
				t.Fatal(err)
			}
			put(t, filepath.Join(stage, "dep", "dep.go"), "package dep\n")
			w, unlock, err := openLocked(root)
			if err != nil {
				t.Fatal(err)
			}
			hashes, err := treeHashes(stage)
			if err != nil {
				unlock()
				t.Fatal(err)
			}
			data, err := encodeManifest(&manifest{Version: stateVersion, Root: ".", Files: map[string]record{"dep/dep.go": {Baseline: diskFile{}, OutputHash: contentHash([]byte("package dep\n"))}}})
			if err != nil {
				unlock()
				t.Fatal(err)
			}
			j := initialJournal{Version: stateVersion, Root: w.root, Stage: filepath.Base(stage), Hashes: hashes, Manifest: data}
			raw, _ := json.Marshal(j)
			if err = os.WriteFile(w.initialPath(), raw, 0600); err != nil {
				unlock()
				t.Fatal(err)
			}
			if point != "before-rename" {
				if err = os.Rename(stage, root); err != nil {
					unlock()
					t.Fatal(err)
				}
			}
			if point == "after-manifest" {
				if err = writeAtomic(w.manifestPath(), data); err != nil {
					unlock()
					t.Fatal(err)
				}
			}
			unlock()
			if _, err = Overlay(root, t.TempDir()); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("initial delivery was not rolled back")
			}
			if _, err = os.Stat(stage); !os.IsNotExist(err) {
				t.Fatal("managed initial staging tree was not removed")
			}
		})
	}
}

func TestInitialRecoveryPreservesUserChanges(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "vendor")
	stage, err := os.MkdirTemp(base, ".goinject-vendor-")
	if err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(stage, "dep.go"), "generated")
	w, unlock, err := openLocked(root)
	if err != nil {
		t.Fatal(err)
	}
	hashes, _ := treeHashes(stage)
	j := initialJournal{Version: stateVersion, Root: w.root, Stage: filepath.Base(stage), Hashes: hashes}
	raw, _ := json.Marshal(j)
	if err = os.WriteFile(w.initialPath(), raw, 0600); err != nil {
		unlock()
		t.Fatal(err)
	}
	if err = os.Rename(stage, root); err != nil {
		unlock()
		t.Fatal(err)
	}
	unlock()
	put(t, filepath.Join(root, "dep.go"), "user patch")
	if _, err = Overlay(root, t.TempDir()); !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	wantFile(t, filepath.Join(root, "dep.go"), "user patch")
}

func TestStalePlanDoesNotOverwriteNewUserPatch(t *testing.T) {
	root := newVendor(t)
	name := filepath.Join(root, "dep.go")
	put(t, name, "new user patch")
	err := ApplyPlan(root, map[string][]byte{name: []byte("generated from old source")}, Plan{Inputs: map[string]Input{name: {Exists: true, Hash: contentHash([]byte("old source"))}}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want stale plan conflict, got %v", err)
	}
	wantFile(t, name, "new user patch")
}
