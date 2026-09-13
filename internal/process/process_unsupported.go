//go:build !linux && !darwin && !windows

package process

import (
	"fmt"
	"os/exec"
)

func newTree(*exec.Cmd) (processTree, error) {
	return nil, fmt.Errorf("process-tree cancellation is supported on Linux, macOS, and Windows")
}
