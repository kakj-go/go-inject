package vendorstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type fileChange struct {
	Path   string   `json:"path"`
	Before diskFile `json:"before"`
	After  diskFile `json:"after"`
}

// A journal is durable before any source file changes. An incomplete
// transaction is always rolled back, even if the new manifest was written.
// Recovery is idempotent, so a process may also be interrupted during rollback.
type journal struct {
	Version        int          `json:"version"`
	Root           string       `json:"root"`
	BeforeManifest diskFile     `json:"beforeManifest"`
	AfterManifest  diskFile     `json:"afterManifest"`
	Changes        []fileChange `json:"changes"`
	CreatedDirs    []string     `json:"createdDirectories,omitempty"`
}

func (w *workspace) commit(j *journal) error {
	if len(j.Changes) == 0 && sameFile(j.BeforeManifest, j.AfterManifest) {
		return nil
	}
	if err := w.prepareJournal(j); err != nil {
		return err
	}
	if err := w.applyJournal(j); err != nil {
		if rollbackErr := w.rollback(j); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("vendor rollback incomplete; retry after resolving the reported conflict: %w", rollbackErr))
		}
		return err
	}
	if err := os.Remove(w.journalPath()); err != nil {
		if rollbackErr := w.rollback(j); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		return fmt.Errorf("finish vendor transaction: %w", err)
	}
	return syncDirectory(w.stateDir)
}

func (w *workspace) prepareJournal(j *journal) error {
	if err := w.validateJournal(j); err != nil {
		return err
	}
	if err := w.checkTransactionFiles(j, false); err != nil {
		return err
	}
	dirs := make(map[string]bool)
	for _, change := range j.Changes {
		if !change.After.Exists {
			continue
		}
		for dir := filepath.Dir(filepath.Join(w.root, filepath.FromSlash(change.Path))); ; dir = filepath.Dir(dir) {
			info, err := os.Lstat(dir)
			if err == nil {
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("refusing vendor output directory %q", dir)
				}
				break
			}
			if !os.IsNotExist(err) {
				return err
			}
			if !within(w.root, dir) {
				return fmt.Errorf("vendor root parent does not exist: %q", dir)
			}
			rel, err := filepath.Rel(w.root, dir)
			if err != nil {
				return err
			}
			dirs[filepath.ToSlash(rel)] = true
			if dir == w.root {
				break
			}
		}
	}
	j.CreatedDirs = sortedKeys(dirs)
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	if _, err := os.Lstat(w.journalPath()); err == nil {
		return fmt.Errorf("pending vendor transaction must be recovered before writing another")
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeAtomic(w.journalPath(), diskFile{Exists: true, Data: append(data, '\n'), Mode: 0o600})
}

func (w *workspace) applyJournal(j *journal) error {
	for _, rel := range j.CreatedDirs {
		if err := mkdirPlain(filepath.Join(w.root, filepath.FromSlash(rel)), 0o755); err != nil {
			return err
		}
	}
	for _, change := range j.Changes {
		current, err := w.readSource(change.Path)
		if err != nil {
			return err
		}
		if !sameFile(current, change.Before) {
			return fmt.Errorf("%w: %s changed after transaction validation", ErrConflict, change.Path)
		}
		if err := writeAtomic(filepath.Join(w.root, filepath.FromSlash(change.Path)), change.After); err != nil {
			return fmt.Errorf("write vendor file %q: %w", change.Path, err)
		}
	}
	return writeAtomic(w.manifestPath(), j.AfterManifest)
}

func (w *workspace) recover() error {
	file, err := readPlain(w.journalPath())
	if err != nil || !file.Exists {
		return err
	}
	var j journal
	if err := decode(file.Data, &j); err != nil {
		return fmt.Errorf("invalid vendor recovery journal: %w", err)
	}
	if err := w.validateJournal(&j); err != nil {
		return err
	}
	if err := w.rollback(&j); err != nil {
		return fmt.Errorf("recover interrupted vendor transaction: %w", err)
	}
	return nil
}

func (w *workspace) rollback(j *journal) error {
	// Preflight every path before undoing anything. If a user changed a file
	// after interruption, recovery must preserve it and the recovery journal.
	if err := w.checkTransactionFiles(j, true); err != nil {
		return err
	}
	for i := len(j.Changes) - 1; i >= 0; i-- {
		change := j.Changes[i]
		current, err := w.readSource(change.Path)
		if err != nil {
			return err
		}
		if !sameContents(current, change.Before) && !sameContents(current, change.After) {
			return fmt.Errorf("%w: %s changed during recovery", ErrConflict, change.Path)
		}
		if !sameFile(current, change.Before) {
			filename := filepath.Join(w.root, filepath.FromSlash(change.Path))
			if change.Before.Exists {
				if err := mkdirPlain(filepath.Dir(filename), 0o755); err != nil {
					return err
				}
			}
			if err := writeAtomic(filename, change.Before); err != nil {
				return fmt.Errorf("restore vendor file %q: %w", change.Path, err)
			}
		}
	}
	if err := writeAtomic(w.manifestPath(), j.BeforeManifest); err != nil {
		return err
	}
	// Remove only directories made by this transaction, and only if empty.
	// Files created by users during an interruption are left in place.
	for i := len(j.CreatedDirs) - 1; i >= 0; i-- {
		dir := filepath.Join(w.root, filepath.FromSlash(j.CreatedDirs[i]))
		if err := checkDirectory(dir); err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	if err := os.Remove(w.journalPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncDirectory(w.stateDir)
}

func (w *workspace) checkTransactionFiles(j *journal, recovering bool) error {
	for _, change := range j.Changes {
		current, err := w.readSource(change.Path)
		if err != nil {
			return err
		}
		matches := sameFile(current, change.Before)
		if recovering {
			matches = sameContents(current, change.Before) || sameContents(current, change.After)
		}
		if !matches {
			return fmt.Errorf("%w: %s differs from the transaction's original and generated content", ErrConflict, change.Path)
		}
	}
	current, err := readPlain(w.manifestPath())
	if err != nil {
		return err
	}
	if !sameContents(current, j.BeforeManifest) && !(recovering && sameContents(current, j.AfterManifest)) {
		return fmt.Errorf("%w: vendor manifest changed during transaction", ErrConflict)
	}
	return nil
}

func (w *workspace) validateJournal(j *journal) error {
	if j.Version != stateVersion || pathIdentity(j.Root) != pathIdentity(w.root) {
		return fmt.Errorf("vendor journal belongs to a different root or unsupported format")
	}
	seen := make(map[string]bool)
	for _, change := range j.Changes {
		if err := validRelative(change.Path); err != nil {
			return err
		}
		identity := pathIdentity(change.Path)
		if seen[identity] {
			return fmt.Errorf("duplicate vendor transaction path %q", change.Path)
		}
		seen[identity] = true
		if err := validateDiskFile(change.Before); err != nil {
			return err
		}
		if err := validateDiskFile(change.After); err != nil {
			return err
		}
	}
	for _, rel := range j.CreatedDirs {
		if rel == "." {
			continue // The vendor root itself may have been absent.
		}
		if err := validRelative(rel); err != nil {
			return fmt.Errorf("invalid transaction directory: %w", err)
		}
	}
	for _, file := range []diskFile{j.BeforeManifest, j.AfterManifest} {
		if err := validateDiskFile(file); err != nil {
			return err
		}
		if !file.Exists {
			continue
		}
		var m manifest
		if err := decode(file.Data, &m); err != nil {
			return err
		}
		if err := w.validateManifest(&m); err != nil {
			return err
		}
	}
	return nil
}

func writeAtomic(filename string, file diskFile) (err error) {
	if err := checkPlainFile(filename); err != nil {
		return err
	}
	if !file.Exists {
		if err := removeFile(filename); err != nil && !os.IsNotExist(err) {
			return err
		}
		return syncDirectory(filepath.Dir(filename))
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".goinject-write-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		temporary.Close()
		os.Chmod(temporaryPath, 0o600)
		os.Remove(temporaryPath)
	}()
	if _, err := temporary.Write(file.Data); err != nil {
		return err
	}
	if err := temporary.Chmod(os.FileMode(file.Mode)); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := replaceFile(temporaryPath, filename); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(filename))
}
