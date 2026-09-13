//go:build !linux && !darwin && !windows

package native

import "os/exec"

func background(*exec.Cmd) {}
