//go:build linux || darwin

package process

import (
	"os"
	"os/exec"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

type unixTree struct {
	mu        sync.Mutex
	pid       int
	requested bool
	closed    bool
}

func newTree(cmd *exec.Cmd) (processTree, error) {
	attributes := new(syscall.SysProcAttr)
	if cmd.SysProcAttr != nil {
		*attributes = *cmd.SysProcAttr
	}
	attributes.Pgid = 0
	// setsid already creates a new process group; setpgid after setsid would
	// fail because the new process is now a session leader.
	attributes.Setpgid = !attributes.Setsid
	cmd.SysProcAttr = attributes
	return new(unixTree), nil
}

func (t *unixTree) started(process *os.Process) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pid = process.Pid
	if t.requested {
		return t.killGroup()
	}
	return nil
}

func (t *unixTree) kill() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return os.ErrProcessDone
	}
	t.requested = true
	return t.killGroup()
}

func (t *unixTree) killGroup() error {
	if t.pid == 0 {
		return nil // Cancellation is remembered until Start returns the PID.
	}
	err := unix.Kill(-t.pid, unix.SIGKILL)
	if err == unix.ESRCH {
		return nil // The complete group has already exited.
	}
	return err
}

func (t *unixTree) close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}
