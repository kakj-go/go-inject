// Package vendorstate preserves the source baseline of files rewritten in a
// vendor directory. State is local to the workspace and is not a replacement
// for version control.
package vendorstate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ErrConflict means a managed file was changed outside this package. No such
// file is overwritten, including while recovering an interrupted transaction.
var ErrConflict = errors.New("vendor source conflict")

type Selection struct{ ID, Target, Version, State string }
type Entry struct {
	ImportPath, GoVersion, GOOS, GOARCH string
	Rules                               []Selection
}
type Input struct {
	Exists bool
	Hash   string
}
type Plan struct {
	Fingerprint string
	Entries     []Entry
	Owners      map[string][]string
	Inputs      map[string]Input
}

// Overlay returns an overlay of saved baseline sources. Files originally
// created by the tool map to an empty path, which hides them from cmd/go.
// Baselines are copied into a fresh directory underneath destDir so the result
// remains valid after the vendor lock is released or the next plan is applied.
// An interrupted transaction is recovered before producing the overlay.
func Overlay(vendorRoot, destDir string) (map[string]string, error) {
	w, unlock, err := openLocked(vendorRoot)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := w.recover(); err != nil {
		return nil, err
	}
	m, _, err := w.readManifest()
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	if m == nil {
		return result, nil
	}
	if _, err := w.checkManaged(m); err != nil {
		return nil, err
	}

	destination, err := absoluteDirectory(destDir)
	if err != nil {
		return nil, fmt.Errorf("vendor baseline destination: %w", err)
	}
	if within(w.root, destination) || within(w.stateDir, destination) {
		return nil, fmt.Errorf("vendor baseline destination %q must be outside the vendor and state directories", destination)
	}
	if err := mkdirPlain(destination, 0o755); err != nil {
		return nil, err
	}
	copyRoot, err := os.MkdirTemp(destination, "vendor-baseline-")
	if err != nil {
		return nil, fmt.Errorf("create vendor baseline copy: %w", err)
	}
	for _, rel := range sortedKeys(m.Files) {
		entry := m.Files[rel]
		original := filepath.Join(w.logicalRoot, filepath.FromSlash(rel))
		if !entry.Baseline.Exists {
			result[original] = ""
			continue
		}
		backing := filepath.Join(copyRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(backing), 0o755); err != nil {
			return nil, fmt.Errorf("create vendor baseline directory: %w", err)
		}
		if err := os.WriteFile(backing, entry.Baseline.Data, 0o600); err != nil {
			return nil, fmt.Errorf("copy vendor baseline %q: %w", rel, err)
		}
		result[original] = backing
	}
	return result, nil
}

// Apply installs one complete plan. outputs contains only changed or newly
// generated files, with absolute paths inside vendorRoot. A missing output from
// the previous plan is restored to its baseline, or removed if the tool created
// it. Existing, previously unmanaged files become user-owned source baselines;
// callers must reject semantic conflicts such as a new helper name already
// belonging to the application before calling Apply.
func Apply(vendorRoot string, outputs map[string][]byte, fingerprint string) error {
	return ApplyPlan(vendorRoot, outputs, Plan{Fingerprint: fingerprint})
}

func ApplyPlan(vendorRoot string, outputs map[string][]byte, plan Plan) error {
	w, unlock, err := openLocked(vendorRoot)
	if err != nil {
		return err
	}
	defer unlock()
	return w.apply(outputs, plan)
}

func (w *workspace) apply(outputs map[string][]byte, plan Plan) error {
	if err := w.recover(); err != nil {
		return err
	}
	old, beforeManifest, err := w.readManifest()
	if err != nil {
		return err
	}
	current, err := w.checkManaged(old)
	if err != nil {
		return err
	}
	if err = w.checkInputs(old, plan.Inputs); err != nil {
		return err
	}
	normalized := make(map[string][]byte, len(outputs))
	identities := make(map[string]string, len(outputs))
	previousSpelling := make(map[string]string)
	if old != nil {
		for rel := range old.Files {
			previousSpelling[pathIdentity(rel)] = rel
		}
	}
	for filename, data := range outputs {
		rel, err := w.relative(filename)
		if err != nil {
			return err
		}
		identity := pathIdentity(rel)
		if previous, ok := identities[identity]; ok {
			return fmt.Errorf("duplicate vendor output paths %q and %q", previous, filename)
		}
		identities[identity] = filename
		if canonical, ok := previousSpelling[identity]; ok {
			rel = canonical
		}
		normalized[rel] = append([]byte(nil), data...)
	}

	next := &manifest{Version: stateVersion, Root: ".", Fingerprint: plan.Fingerprint, Plan: plan.Entries, Files: make(map[string]record)}
	changes := make(map[string]fileChange)
	if old != nil {
		for rel, entry := range old.Files {
			changes[rel] = fileChange{Path: rel, Before: current[rel], After: entry.Baseline}
		}
	}
	for _, rel := range sortedKeys(normalized) {
		before, known := current[rel]
		if !known {
			before, err = w.readSource(rel)
			if err != nil {
				return err
			}
		}
		baseline := before
		if old != nil {
			if previous, ok := old.Files[rel]; ok {
				baseline = previous.Baseline
			}
		}
		mode := before.Mode
		if !before.Exists {
			mode = 0o644
		}
		after := diskFile{Exists: true, Data: normalized[rel], Mode: mode}
		// A plan which reproduces the baseline no longer owns this file.
		if !sameContents(baseline, after) {
			owners := plan.Owners[filepath.Join(w.logicalRoot, filepath.FromSlash(rel))]
			next.Files[rel] = record{Baseline: baseline, OutputHash: contentHash(after.Data), Owners: owners}
		}
		changes[rel] = fileChange{Path: rel, Before: before, After: after}
	}
	afterManifest, err := encodeManifest(next)
	if err != nil {
		return err
	}
	transaction := &journal{Version: stateVersion, Root: w.root, BeforeManifest: beforeManifest, AfterManifest: afterManifest}
	for _, rel := range sortedKeys(changes) {
		change := changes[rel]
		if !sameFile(change.Before, change.After) {
			transaction.Changes = append(transaction.Changes, change)
		}
	}
	return w.commit(transaction)
}

func (w *workspace) checkInputs(m *manifest, inputs map[string]Input) error {
	for name, want := range inputs {
		rel, err := w.relative(name)
		if err != nil {
			return err
		}
		actual, err := w.readSource(rel)
		if err != nil {
			return err
		}
		if m != nil {
			if item, ok := m.Files[rel]; ok {
				actual = item.Baseline
			}
		}
		if actual.Exists != want.Exists || actual.Exists && contentHash(actual.Data) != want.Hash {
			return fmt.Errorf("%w: %s changed while planning", ErrConflict, name)
		}
	}
	return nil
}

// Restore removes all tool changes and forgets the saved baseline after a
// successful transaction. Unrelated files remain unchanged.
func Restore(vendorRoot string) error {
	return Apply(vendorRoot, nil, "")
}

// HasState reports whether a baseline manifest or pending transaction exists.
// It is a read-only hint; Overlay, Apply, and Restore acquire the process lock
// and perform authoritative validation and recovery.
func HasState(vendorRoot string) bool {
	w, err := locate(vendorRoot)
	if err != nil {
		return false
	}
	for _, filename := range []string{w.manifestPath(), w.journalPath(), w.initialPath()} {
		if _, err := os.Lstat(filename); err == nil {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
