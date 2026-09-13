package vendorstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

type initialJournal struct {
	Version  int               `json:"version"`
	Root     string            `json:"root"`
	Stage    string            `json:"stage"`
	Hashes   map[string]string `json:"hashes"`
	Manifest diskFile          `json:"manifest"`
}

func (w *workspace) initialPath() string { return filepath.Join(w.stateDir, "initial.json") }

// Install commits a completely generated sibling staging tree and its portable
// state. A durable journal precedes the directory rename, including the first
// creation of vendor; failures recover to the previous absent directory.
func Install(root, stage string, outputs map[string][]byte, plan Plan) error {
	w, unlock, err := openLocked(root)
	if err != nil {
		return err
	}
	defer unlock()
	if err = w.recover(); err != nil {
		return err
	}
	if _, err = os.Lstat(w.root); err == nil {
		return fmt.Errorf("%w: vendor appeared while generating", ErrConflict)
	} else if !os.IsNotExist(err) {
		return err
	}
	stage, err = absoluteDirectory(stage)
	if err != nil {
		return err
	}
	if filepath.Dir(stage) != filepath.Dir(w.root) || !strings.HasPrefix(filepath.Base(stage), ".goinject-vendor-") {
		return fmt.Errorf("vendor stage must be a managed sibling directory")
	}
	old, _, err := w.readManifest()
	if err != nil {
		return err
	}
	if old != nil {
		return fmt.Errorf("%w: vendor is absent but saved state remains", ErrConflict)
	}
	staging := &workspace{root: stage, logicalRoot: stage, stateDir: w.stateDir}
	next := &manifest{Version: stateVersion, Root: ".", Fingerprint: plan.Fingerprint, Plan: plan.Entries, Files: map[string]record{}}
	for name, want := range plan.Inputs {
		rel, err := w.relative(name)
		if err != nil {
			return err
		}
		file, err := staging.readSource(rel)
		if err != nil {
			return err
		}
		if file.Exists != want.Exists || file.Exists && contentHash(file.Data) != want.Hash {
			return fmt.Errorf("%w: vendor baseline differs from planned input %s", ErrConflict, rel)
		}
	}
	for _, name := range sortedKeys(outputs) {
		rel, err := w.relative(name)
		if err != nil {
			return err
		}
		before, err := staging.readSource(rel)
		if err != nil {
			return err
		}
		mode := before.Mode
		if !before.Exists {
			mode = 0644
		}
		after := diskFile{Exists: true, Data: outputs[name], Mode: mode}
		if !sameContents(before, after) {
			next.Files[rel] = record{Baseline: before, OutputHash: contentHash(after.Data), Owners: plan.Owners[name]}
		}
		path := filepath.Join(stage, filepath.FromSlash(rel))
		if err = mkdirPlain(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err = writeAtomic(path, after); err != nil {
			return err
		}
	}
	state, err := encodeManifest(next)
	if err != nil {
		return err
	}
	hashes, err := treeHashes(stage)
	if err != nil {
		return err
	}
	j := initialJournal{Version: stateVersion, Root: w.root, Stage: filepath.Base(stage), Hashes: hashes, Manifest: state}
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	if err = writeAtomic(w.initialPath(), diskFile{Exists: true, Data: data, Mode: 0600}); err != nil {
		return err
	}
	if err = os.Rename(stage, w.root); err != nil {
		_ = removeFile(w.initialPath())
		return err
	}
	if err = writeAtomic(w.manifestPath(), state); err == nil {
		err = removeFile(w.initialPath())
	}
	if err != nil {
		if recovery := w.recoverInitial(); recovery != nil {
			return errors.Join(err, recovery)
		}
		return err
	}
	return syncDirectory(w.stateDir)
}

func treeHashes(root string) (map[string]string, error) {
	hashes := map[string]string{}
	err := filepath.WalkDir(root, func(name string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing vendor tree symlink %s", name)
		}
		if d.IsDir() {
			return nil
		}
		file, err := readPlain(name)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		hashes[filepath.ToSlash(rel)] = fmt.Sprintf("%s/%o", contentHash(file.Data), file.Mode)
		return nil
	})
	return hashes, err
}

func (w *workspace) recoverInitial() error {
	state, err := readPlain(w.initialPath())
	if err != nil || !state.Exists {
		return err
	}
	var j initialJournal
	if err = decode(state.Data, &j); err != nil {
		return fmt.Errorf("invalid initial vendor journal: %w", err)
	}
	if j.Version != stateVersion || pathIdentity(j.Root) != pathIdentity(w.root) || filepath.Base(j.Stage) != j.Stage || !strings.HasPrefix(j.Stage, ".goinject-vendor-") {
		return fmt.Errorf("invalid initial vendor journal ownership")
	}
	for rel := range j.Hashes {
		if err = validRelative(rel); err != nil {
			return err
		}
	}
	stage := filepath.Join(filepath.Dir(w.root), j.Stage)
	manifest, err := readPlain(w.manifestPath())
	if err != nil {
		return err
	}
	if manifest.Exists && !sameContents(manifest, j.Manifest) {
		return fmt.Errorf("%w: initial vendor manifest was edited", ErrConflict)
	}
	if _, err = os.Lstat(w.root); err == nil {
		hashes, e := treeHashes(w.root)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(hashes, j.Hashes) {
			return fmt.Errorf("%w: initial vendor output was edited after interruption", ErrConflict)
		}
		if _, e = os.Lstat(stage); !os.IsNotExist(e) {
			return fmt.Errorf("%w: initial stage is occupied", ErrConflict)
		}
		if e = os.Rename(w.root, stage); e != nil {
			return e
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if manifest.Exists {
		if err = removeFile(w.manifestPath()); err != nil {
			return err
		}
	}
	if _, err = os.Lstat(stage); err == nil {
		hashes, e := treeHashes(stage)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(hashes, j.Hashes) {
			return fmt.Errorf("%w: initial stage was edited", ErrConflict)
		}
		// Both the path and complete contents were checked; this is solely the
		// transaction's newly created sibling staging tree.
		if e = os.RemoveAll(stage); e != nil {
			return e
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = removeFile(w.initialPath()); err != nil {
		return err
	}
	return syncDirectory(w.stateDir)
}
