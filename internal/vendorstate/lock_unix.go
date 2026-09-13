//go:build linux || darwin

package vendorstate

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockFile(filename string) (func(), error) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = unix.Flock(int(file.Fd()), unix.LOCK_EX)
		if err != unix.EINTR {
			break
		}
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return func() {
		unix.Flock(int(file.Fd()), unix.LOCK_UN)
		file.Close()
	}, nil
}

func replaceFile(source, destination string) error { return os.Rename(source, destination) }
func removeFile(filename string) error             { return os.Remove(filename) }

func syncDirectory(name string) error {
	dir, err := os.Open(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
