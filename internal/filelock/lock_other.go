//go:build !windows && !linux && !darwin

package filelock

import "fmt"

func Lock(string) (func(), error) { return nil, fmt.Errorf("unsupported file-lock platform") }
