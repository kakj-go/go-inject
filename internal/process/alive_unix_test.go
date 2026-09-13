//go:build linux || darwin

package process

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

func processExited(pid int) (bool, error) {
	if runtime.GOOS == "linux" {
		// A killed orphan can remain a zombie until PID 1 reaps it, especially
		// in containers. A zombie has exited and cannot execute compiler work.
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if os.IsNotExist(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		end := strings.LastIndex(string(data), ") ")
		if end >= 0 && len(data) > end+2 && data[end+2] == 'Z' {
			return true, nil
		}
	}
	err := unix.Kill(pid, 0)
	if err == unix.ESRCH {
		return true, nil
	}
	return false, err
}
