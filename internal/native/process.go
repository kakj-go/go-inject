package native

import "github.com/shirou/gopsutil/v4/process"

func processTime(pid int32) (int64, error) {
	p, e := process.NewProcess(pid)
	if e != nil {
		return 0, e
	}
	return p.CreateTime()
}
