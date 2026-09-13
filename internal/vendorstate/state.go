package vendorstate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const stateVersion = 2

type diskFile struct {
	Exists bool   `json:"exists"`
	Data   []byte `json:"data,omitempty"`
	Mode   uint32 `json:"mode,omitempty"`
}

type record struct {
	Baseline   diskFile `json:"baseline"`
	OutputHash string   `json:"outputHash"`
	Owners     []string `json:"owners,omitempty"`
}

type manifest struct {
	Version     int               `json:"version"`
	Root        string            `json:"root"`
	Fingerprint string            `json:"fingerprint"`
	Files       map[string]record `json:"files"`
	Plan        []Entry           `json:"plan,omitempty"`
}

type workspace struct {
	root        string
	logicalRoot string
	stateDir    string
}

func (w *workspace) manifestPath() string { return filepath.Join(w.stateDir, "manifest.json") }
func (w *workspace) journalPath() string  { return filepath.Join(w.stateDir, "transaction.json") }

func openLocked(root string) (*workspace, func(), error) {
	w, err := locate(root)
	if err != nil {
		return nil, nil, err
	}
	if err := checkDirectory(filepath.Dir(w.stateDir)); err != nil {
		return nil, nil, err
	}
	if err := mkdirPlain(w.stateDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create vendor state directory: %w", err)
	}
	lockPath := filepath.Join(w.stateDir, "lock")
	if err := checkPlainFile(lockPath); err != nil {
		return nil, nil, err
	}
	unlock, err := lockFile(lockPath)
	if err != nil {
		return nil, nil, fmt.Errorf("lock vendor state: %w", err)
	}
	return w, unlock, nil
}

func (w *workspace) readManifest() (*manifest, diskFile, error) {
	file, err := readPlain(w.manifestPath())
	if err != nil || !file.Exists {
		return nil, file, err
	}
	var m manifest
	if err := decode(file.Data, &m); err != nil {
		return nil, file, fmt.Errorf("invalid vendor manifest: %w", err)
	}
	if err := w.validateManifest(&m); err != nil {
		return nil, file, err
	}
	return &m, file, nil
}

func (w *workspace) validateManifest(m *manifest) error {
	if m.Version != stateVersion || m.Root != "." {
		return fmt.Errorf("vendor state belongs to a different root or unsupported format: root=%q version=%d", m.Root, m.Version)
	}
	seen := make(map[string]bool, len(m.Files))
	for rel, item := range m.Files {
		if err := validRelative(rel); err != nil {
			return fmt.Errorf("invalid vendor state path: %w", err)
		}
		if seen[pathIdentity(rel)] {
			return fmt.Errorf("duplicate vendor state path %q", rel)
		}
		seen[pathIdentity(rel)] = true
		hash, err := hex.DecodeString(item.OutputHash)
		if err != nil || len(hash) != sha256.Size {
			return fmt.Errorf("invalid vendor output hash for %q", rel)
		}
		if err := validateDiskFile(item.Baseline); err != nil {
			return fmt.Errorf("invalid vendor baseline for %q: %w", rel, err)
		}
	}
	return nil
}

func (w *workspace) checkManaged(m *manifest) (map[string]diskFile, error) {
	current := make(map[string]diskFile)
	if m == nil {
		return current, nil
	}
	for _, rel := range sortedKeys(m.Files) {
		item := m.Files[rel]
		file, err := w.readSource(rel)
		if err != nil {
			return nil, err
		}
		if !sameContents(file, item.Baseline) && (!file.Exists || contentHash(file.Data) != item.OutputHash) {
			return nil, fmt.Errorf("%w: %s differs from its saved baseline and previous generated output", ErrConflict, filepath.Join(w.root, filepath.FromSlash(rel)))
		}
		current[rel] = file
	}
	return current, nil
}

func encodeManifest(m *manifest) (diskFile, error) {
	if len(m.Files) == 0 {
		return diskFile{}, nil
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return diskFile{}, err
	}
	return diskFile{Exists: true, Data: append(data, '\n'), Mode: 0o600}, nil
}

func decode(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("unexpected trailing JSON data")
	}
	return nil
}

func contentHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func sameContents(a, b diskFile) bool {
	return a.Exists == b.Exists && (!a.Exists || bytes.Equal(a.Data, b.Data))
}

func sameFile(a, b diskFile) bool {
	return sameContents(a, b) && (!a.Exists || a.Mode == b.Mode)
}

func validateDiskFile(file diskFile) error {
	if !file.Exists && (len(file.Data) != 0 || file.Mode != 0) {
		return fmt.Errorf("absent file contains data or mode")
	}
	if file.Mode & ^uint32(os.ModePerm) != 0 {
		return fmt.Errorf("unsupported file mode %o", file.Mode)
	}
	return nil
}

func readPlain(filename string) (diskFile, error) {
	info, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return diskFile{}, nil
	}
	if err != nil {
		return diskFile{}, err
	}
	if !info.Mode().IsRegular() {
		return diskFile{}, fmt.Errorf("refusing non-regular file %q", filename)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return diskFile{}, err
	}
	return diskFile{Exists: true, Data: data, Mode: uint32(info.Mode().Perm())}, nil
}
