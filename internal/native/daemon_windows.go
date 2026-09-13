package native

import (
	"golang.org/x/sys/windows"
	"os/exec"
	"syscall"
)

func background(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}
