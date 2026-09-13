//go:build linux || darwin

package native

import (
	"os/exec"
	"syscall"
)

func background(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
