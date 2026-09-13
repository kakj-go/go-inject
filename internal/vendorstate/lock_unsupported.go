//go:build !linux && !darwin && !windows

package vendorstate

import "fmt"

func lockFile(string) (func(), error) {
	return nil, fmt.Errorf("vendor transactions are supported on Linux, macOS, and Windows")
}
func replaceFile(string, string) error { return fmt.Errorf("unsupported vendor transaction platform") }
func removeFile(string) error          { return fmt.Errorf("unsupported vendor transaction platform") }
func syncDirectory(string) error       { return nil }
