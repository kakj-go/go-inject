package process

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func processExited(pid int) (bool, error) {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err == windows.ERROR_INVALID_PARAMETER {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(handle)
	status, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return false, err
	}
	switch status {
	case windows.WAIT_OBJECT_0:
		return true, nil
	case uint32(windows.WAIT_TIMEOUT):
		return false, nil
	default:
		return false, fmt.Errorf("unexpected process wait status %d", status)
	}
}
