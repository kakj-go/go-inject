package vendorstate

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestSharingViolationRollsBackEarlierWrites(t *testing.T) {
	root := newVendor(t)
	a := filepath.Join(root, "a.go")
	b := filepath.Join(root, "b.go")
	put(t, a, "original-a")
	put(t, b, "original-b")
	name, err := windows.UTF16PtrFromString(b)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	err = Apply(root, map[string][]byte{a: []byte("new-a"), b: []byte("new-b")}, "blocked-write")
	if err == nil {
		t.Fatal("expected replacement to fail while destination denies delete-sharing")
	}
	wantFile(t, a, "original-a")
	wantFile(t, b, "original-b")
	if HasState(root) {
		t.Fatal("successful rollback left active state")
	}
}
