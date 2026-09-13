package vendorstate

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockFile(filename string) (func(), error) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	overlapped := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped); err != nil {
		file.Close()
		return nil, err
	}
	return func() {
		windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
		file.Close()
	}, nil
}

func replaceFile(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	restoreMode, err := makeWritable(destination)
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
	if err != nil {
		restoreMode()
	}
	return err
}

func removeFile(filename string) error {
	restoreMode, err := makeWritable(filename)
	if err != nil {
		return err
	}
	err = os.Remove(filename)
	if err != nil {
		restoreMode()
	}
	return err
}

// NTFS refuses to replace or delete read-only files. The transaction already
// records the original mode, so recovery can also undo an interruption between
// clearing this attribute and replacing the directory entry.
func makeWritable(filename string) (func(), error) {
	info, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return func() {}, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o222 != 0 {
		return func() {}, nil
	}
	if err := os.Chmod(filename, info.Mode().Perm()|0o200); err != nil {
		return nil, err
	}
	return func() { os.Chmod(filename, info.Mode().Perm()) }, nil
}

// MoveFileEx with WRITE_THROUGH makes replacement durable. Opening a Windows
// directory and calling os.File.Sync is not supported on ordinary directory
// handles, so the Unix directory-fsync step has no counterpart here.
func syncDirectory(string) error { return nil }
