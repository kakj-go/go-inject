package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const helperMode = "GOINJECT_PROCESS_TEST_MODE"
const helperDir = "GOINJECT_PROCESS_TEST_DIR"

func testCommand(t *testing.T, mode, dir string) *exec.Cmd {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestRunHelper$", "-test.timeout=20s")
	cmd.Env = append(os.Environ(), helperMode+"="+mode, helperDir+"="+dir)
	return cmd
}

func TestRunHelper(t *testing.T) {
	mode := os.Getenv(helperMode)
	if mode == "" {
		return
	}
	dir := os.Getenv(helperDir)
	writePID := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name+".pid"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	switch mode {
	case "success":
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(os.Stdout, "success:%s:%s", os.Getenv("GOINJECT_PROCESS_TEST_VALUE"), cwd)
		fmt.Fprint(os.Stderr, "configured stderr")
		os.Exit(0)
	case "exit":
		os.Exit(29)
	case "leaf":
		writePID("grandchild")
		for {
			time.Sleep(time.Hour)
		}
	case "parent", "middle":
		next := "middle"
		name := "parent"
		if mode == "middle" {
			next = "leaf"
			name = "child"
		}
		child := testCommand(t, next, dir)
		// Inheriting these descriptors makes a process-only kill insufficient:
		// exec.Cmd.Wait will also be waiting for descendants to close the pipes.
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		writePID(name)
		child.Wait()
		os.Exit(0)
	default:
		t.Fatalf("unknown helper mode %q", mode)
	}
}

func TestRunPreservesConfiguredCommand(t *testing.T) {
	dir := t.TempDir()
	cmd := testCommand(t, "success", dir)
	cmd.Dir = dir
	cmd.Env = append(cmd.Env, "GOINJECT_PROCESS_TEST_VALUE=custom")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := Run(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	want := "success:custom:" + dir
	if !strings.EqualFold(stdout.String(), want) {
		// Some systems canonicalize the /var symlink when starting a child.
		canonical, err := filepath.EvalSymlinks(dir)
		if err != nil || !strings.EqualFold(stdout.String(), "success:custom:"+canonical) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
	if stderr.String() != "configured stderr" {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 0 {
		t.Fatalf("process state = %v", cmd.ProcessState)
	}
}

func TestRunPreservesExitError(t *testing.T) {
	cmd := testCommand(t, "exit", t.TempDir())
	err := Run(context.Background(), cmd)
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 29 {
		t.Fatalf("Run = %T %v, want original *exec.ExitError with code 29", err, err)
	}
}

func TestRunCanceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := testCommand(t, "parent", t.TempDir())
	if err := Run(ctx, cmd); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v", err)
	}
	if cmd.Process != nil {
		t.Fatal("pre-canceled command was started")
	}
}

func TestRunStartError(t *testing.T) {
	cmd := exec.Command(filepath.Join(t.TempDir(), "missing-executable"))
	if err := Run(context.Background(), cmd); err == nil {
		t.Fatal("missing executable succeeded")
	}
}

func TestRunRejectsInvalidOrStartedCommands(t *testing.T) {
	if err := Run(nil, exec.Command("unused")); err == nil {
		t.Fatal("nil context accepted")
	}
	if err := Run(context.Background(), nil); err == nil {
		t.Fatal("nil command accepted")
	}
	cmd := testCommand(t, "success", t.TempDir())
	if err := Run(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), cmd); err == nil {
		t.Fatal("already-run command accepted")
	}
}

func waitForTree(t *testing.T, dir string) []int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var pids []int
	for time.Now().Before(deadline) {
		pids = nil
		for _, name := range []string{"parent", "child", "grandchild"} {
			data, err := os.ReadFile(filepath.Join(dir, name+".pid"))
			if err != nil {
				break
			}
			pid, err := strconv.Atoi(string(data))
			if err != nil || pid <= 0 {
				break
			}
			pids = append(pids, pid)
		}
		if len(pids) == 3 {
			return pids
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("helper tree did not become ready: %v", pids)
	return nil
}

func waitForExited(t *testing.T, pids []int) {
	t.Helper()
	for _, pid := range pids {
		deadline := time.Now().Add(5 * time.Second)
		for {
			exited, err := processExited(pid)
			if err != nil {
				t.Fatal(err)
			}
			if exited {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("descendant %d survived cancellation", pid)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestRunCancellationKillsDescendants(t *testing.T) {
	for _, contextKind := range []string{"plain", "shared-command-context", "separate-command-context"} {
		t.Run(contextKind, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cmd := testCommand(t, "parent", dir)
			if contextKind != "plain" {
				withContext := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
				withContext.Env = cmd.Env
				cmd = withContext
			}
			runContext := ctx
			if contextKind == "separate-command-context" {
				runContext = context.Background()
			}
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			done := make(chan error, 1)
			go func() { done <- Run(runContext, cmd) }()
			pids := waitForTree(t, dir)
			t.Cleanup(func() {
				for _, pid := range pids {
					if exited, err := processExited(pid); err == nil && exited {
						continue
					}
					if process, err := os.FindProcess(pid); err == nil {
						process.Kill()
						process.Release()
					}
				}
			})
			cancel()
			select {
			case err := <-done:
				if contextKind != "separate-command-context" && !errors.Is(err, context.Canceled) {
					t.Fatalf("Run = %v, want cancellation; output: %s", err, output.String())
				}
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatalf("cancellation lost process exit error: %T %v", err, err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("Run hung while canceled descendants held inherited I/O pipes")
			}
			waitForExited(t, pids)
		})
	}
}

func TestRunCancellationDuringStartup(t *testing.T) {
	for range 12 {
		ctx, cancel := context.WithCancel(context.Background())
		cmd := testCommand(t, "parent", t.TempDir())
		done := make(chan error, 1)
		go func() { done <- Run(ctx, cmd) }()
		// Exercise cancellation both before Start and around process/job setup.
		time.Sleep(time.Millisecond)
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Run = %v, want cancellation", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("startup cancellation left a suspended or running child")
		}
	}
}
