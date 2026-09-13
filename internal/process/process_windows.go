package process

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var resumeProcess = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess")

type windowsTree struct {
	mu        sync.Mutex
	job       windows.Handle
	process   windows.Handle
	assigned  bool
	requested bool
	closed    bool
}

func newTree(cmd *exec.Cmd) (processTree, error) {
	// os/exec closes the primary thread handle returned by CreateProcess.
	// Resolve NtResumeProcess before creating a suspended child, and resume
	// through an owned process handle only after the job assignment succeeds.
	if err := resumeProcess.Find(); err != nil {
		return nil, fmt.Errorf("find NtResumeProcess: %w", err)
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create command job: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("configure command job: %w", err)
	}
	attributes := new(syscall.SysProcAttr)
	if cmd.SysProcAttr != nil {
		*attributes = *cmd.SysProcAttr
	}
	attributes.HideWindow = true
	attributes.CreationFlags |= windows.CREATE_SUSPENDED
	cmd.SysProcAttr = attributes
	return &windowsTree{job: job}, nil
}

func (t *windowsTree) started(process *os.Process) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	access := uint32(windows.PROCESS_SET_QUOTA | windows.PROCESS_TERMINATE | windows.PROCESS_SUSPEND_RESUME)
	handle, err := windows.OpenProcess(access, false, uint32(process.Pid))
	if err != nil {
		return fmt.Errorf("open suspended process: %w", err)
	}
	t.process = handle
	if err := windows.AssignProcessToJobObject(t.job, handle); err != nil {
		return fmt.Errorf("assign suspended process to job: %w", err)
	}
	t.assigned = true
	if t.requested {
		return t.terminate()
	}
	status, _, _ := resumeProcess.Call(uintptr(handle))
	if int32(status) < 0 {
		return fmt.Errorf("resume command process: %w", windows.NTStatus(status))
	}
	return nil
}

func (t *windowsTree) kill() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return os.ErrProcessDone
	}
	t.requested = true
	return t.terminate()
}

func (t *windowsTree) terminate() error {
	if !t.assigned {
		if t.process != 0 {
			return windows.TerminateProcess(t.process, 1)
		}
		return nil // A child not yet started will be assigned and then killed.
	}
	return windows.TerminateJobObject(t.job, 1)
}

func (t *windowsTree) close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	var err error
	if t.process != 0 {
		err = windows.CloseHandle(t.process)
		t.process = 0
	}
	if t.job != 0 {
		err = join(err, windows.CloseHandle(t.job))
		t.job = 0
	}
	return err
}
