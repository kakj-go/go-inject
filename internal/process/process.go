// Package process runs top-level Go commands in an independently cancellable
// process tree. Compiler subprocesses invoked by toolexec must remain in that
// tree and should use exec.Cmd.Run directly, not this wrapper.
package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

type processTree interface {
	started(*os.Process) error
	kill() error
	close() error
}

type cancellation struct {
	requested bool
	err       error
}

// Run starts cmd and waits for it, terminating its process tree when ctx is
// canceled. It preserves cmd's arguments, environment, directory, and I/O.
// Existing SysProcAttr values are copied before setting process isolation.
// For commands created with exec.CommandContext, Run replaces Cancel with
// process-tree cancellation, so that command's context is also respected.
// Nonzero process exit errors retain their original *exec.ExitError and code;
// cancellation additionally matches ctx.Err through errors.Is.
func Run(ctx context.Context, cmd *exec.Cmd) (err error) {
	if ctx == nil || cmd == nil {
		return fmt.Errorf("process.Run requires a context and command")
	}
	if cmd.Process != nil || cmd.ProcessState != nil {
		return fmt.Errorf("process.Run command has already been started")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tree, err := newTree(cmd)
	if err != nil {
		return err
	}
	if cmd.Cancel != nil {
		cmd.Cancel = tree.kill
	}
	finished := make(chan struct{})
	canceled := make(chan cancellation, 1)
	go func() {
		select {
		case <-finished:
			canceled <- cancellation{}
		case <-ctx.Done():
			canceled <- cancellation{requested: true, err: tree.kill()}
		}
	}()
	defer func() {
		close(finished)
		result := <-canceled
		if result.requested && !errors.Is(result.err, os.ErrProcessDone) {
			err = join(err, ctx.Err())
			if result.err != nil {
				err = join(err, fmt.Errorf("cancel command process tree: %w", result.err))
			}
		}
		if closeErr := tree.close(); closeErr != nil {
			err = join(err, fmt.Errorf("close command process tree: %w", closeErr))
		}
	}()
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := tree.started(cmd.Process); err != nil {
		// On Windows the process is still suspended if assignment or resume
		// failed. Always reap it, including when no job assignment succeeded.
		tree.kill()
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("initialize command process tree: %w", err)
	}
	return cmd.Wait()
}

func join(first, second error) error {
	if first == nil {
		return second
	}
	if second == nil {
		return first
	}
	if errors.Is(first, second) {
		return first
	}
	return errors.Join(first, second)
}
