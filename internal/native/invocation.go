// Package native connects the injection engine to the standard Go toolchain.
package native

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kakj-go/go-inject/internal/project"
	"github.com/shirou/gopsutil/v4/process"
)

type invocation struct {
	PID       int32
	Started   int64
	Dir, Kind string
	Args      []string
}

func discover(ctx context.Context) (invocation, error) {
	p, err := process.NewProcessWithContext(ctx, int32(os.Getpid()))
	if err != nil {
		return invocation{}, err
	}
	for depth := 0; depth < 12; depth++ {
		p, err = p.ParentWithContext(ctx)
		if err != nil {
			return invocation{}, fmt.Errorf("find parent Go invocation: %w", err)
		}
		args, err := p.CmdlineSliceWithContext(ctx)
		if err != nil {
			return invocation{}, fmt.Errorf("read parent command: %w", err)
		}
		if len(args) == 0 {
			continue
		}
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(args[0]), ".exe"))
		if base != "go" {
			continue
		}
		rest := args[1:]
		// cmd/go already changed cwd before invoking any compiler hook.
		if len(rest) > 0 && (rest[0] == "-C" || strings.HasPrefix(rest[0], "-C=")) {
			if rest[0] == "-C" {
				if len(rest) < 2 {
					return invocation{}, fmt.Errorf("missing Go -C argument")
				}
				rest = rest[2:]
			} else {
				rest = rest[1:]
			}
		}
		if len(rest) == 0 || rest[0] != "build" && rest[0] != "test" && rest[0] != "install" {
			continue
		}
		dir, err := p.CwdWithContext(ctx)
		if err != nil {
			return invocation{}, fmt.Errorf("read parent Go working directory: %w", err)
		}
		started, err := p.CreateTimeWithContext(ctx)
		if err != nil {
			return invocation{}, err
		}
		argv := append([]string{}, rest[1:]...)
		if len(argv) > 0 && (argv[0] == "-C" || strings.HasPrefix(argv[0], "-C=")) {
			if argv[0] == "-C" {
				if len(argv) < 2 {
					return invocation{}, fmt.Errorf("missing Go -C argument")
				}
				argv = argv[2:]
			} else {
				argv = argv[1:]
			}
		}
		return invocation{PID: p.Pid, Started: started, Dir: dir, Kind: rest[0], Args: argv}, nil
	}
	return invocation{}, fmt.Errorf("cannot identify parent go build/test/install; invoke this tool using -toolexec=go-inject")
}

func alive(pid int32, started int64) bool {
	p, e := process.NewProcess(pid)
	if e != nil {
		return false
	}
	current, e := p.CreateTime()
	if e != nil || current != started {
		return false
	}
	running, e := p.IsRunning()
	if e != nil || !running {
		return false
	}
	states, e := p.Status()
	if e == nil {
		for _, s := range states {
			if s == process.Zombie || s == "dead" {
				return false
			}
		}
	}
	return true
}

func digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }

func (i invocation) resolve(ctx context.Context) (project.Env, []string, []*project.Package, error) {
	dir, flags, patterns, e := project.ResolveArgs(ctx, i.Dir, i.Args)
	if e != nil {
		return project.Env{}, nil, nil, e
	}
	env, e := project.Environment(ctx, dir)
	if e != nil {
		return env, nil, nil, e
	}
	roots, e := project.List(ctx, dir, project.ListFlags(flags), false, false, patterns...)
	if e != nil {
		return env, nil, nil, e
	}
	if len(roots) == 0 {
		return env, nil, nil, fmt.Errorf("no Go entry packages matched")
	}
	for _, root := range roots {
		if root.Error != nil {
			return env, nil, nil, fmt.Errorf("entry %s: %s", root.Base(), root.Error.Err)
		}
		if root.Base() == "command-line-arguments" {
			for _, file := range patterns {
				if !filepath.IsAbs(file) {
					file = filepath.Join(dir, file)
				}
				root.EntryFiles = append(root.EntryFiles, file)
			}
		}
	}
	return env, flags, roots, nil
}
