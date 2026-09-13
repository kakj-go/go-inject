// Package e2e exercises the public CLI and executes the resulting binaries.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kakj-go/go-inject/internal/process"
)

var cliBinary string
var toolchainGo = filepath.Join(runtime.GOROOT(), "bin", "go"+exeSuffix())

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func TestMain(m *testing.M) {
	cliBinary = os.Getenv("GOINJECT_BINARY")
	var temporary string
	if cliBinary == "" {
		var err error
		// Exercise toolexec quoting with a real executable path containing spaces.
		temporary, err = os.MkdirTemp("", "go inject e2e cli-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		cliBinary = filepath.Join(temporary, "go-inject"+exeSuffix())
		repo, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			_ = os.RemoveAll(temporary)
			os.Exit(1)
		}
		for {
			if _, err := os.Stat(filepath.Join(repo, "cmd", "go-inject", "main.go")); err == nil {
				break
			}
			parent := filepath.Dir(repo)
			if parent == repo {
				fmt.Fprintln(os.Stderr, "cannot locate go-inject repository; set an absolute GOINJECT_BINARY path")
				_ = os.RemoveAll(temporary)
				os.Exit(1)
			}
			repo = parent
		}
		output, err := command(repo, nil, 3*time.Minute, toolchainGo, "build", "-o", cliBinary, "./cmd/go-inject")
		if err != nil {
			fmt.Fprintf(os.Stderr, "build E2E CLI: %v\n%s", err, output)
			_ = os.RemoveAll(temporary)
			os.Exit(1)
		}
	} else {
		var err error
		cliBinary, err = filepath.Abs(cliBinary)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	code := m.Run()
	if temporary != "" {
		_ = os.RemoveAll(temporary)
	}
	os.Exit(code)
}

type fixture struct {
	t   *testing.T
	dir string
	env map[string]string
}

func newFixture(t *testing.T, files map[string]string) *fixture {
	t.Helper()
	f := &fixture{t: t, dir: t.TempDir(), env: map[string]string{"GOPROXY": "off", "GOSUMDB": "off", "GOWORK": "off", "GOFLAGS": ""}}
	if files["go.mod"] == "" {
		files["go.mod"] = "module example.test/app\n\ngo 1.26.0\n"
	}
	for name, contents := range files {
		f.write(name, contents)
	}
	return f
}

func (f *fixture) write(name, contents string) {
	f.t.Helper()
	filename := filepath.Join(f.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0600); err != nil {
		f.t.Fatal(err)
	}
}

func environment(overrides map[string]string) []string {
	values := map[string]string{}
	for _, entry := range os.Environ() {
		if i := strings.IndexByte(entry, '='); i >= 0 {
			key := entry[:i]
			if runtime.GOOS == "windows" {
				key = strings.ToUpper(key)
			}
			values[key] = entry[i+1:]
		}
	}
	values["GOTOOLCHAIN"] = "local"
	values["PATH"] = filepath.Dir(toolchainGo) + string(os.PathListSeparator) + os.Getenv("PATH")
	for key, value := range overrides {
		if runtime.GOOS == "windows" {
			key = strings.ToUpper(key)
		}
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func command(dir string, env map[string]string, timeout time.Duration, exe string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = environment(env)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := process.Run(ctx, cmd)
	if ctx.Err() != nil {
		return output.String(), fmt.Errorf("command timed out after %s: %w", timeout, ctx.Err())
	}
	return output.String(), err
}

func (f *fixture) cli(args ...string) string {
	f.t.Helper()
	output, err := invoke(f.dir, f.env, 6*time.Minute, args...)
	if err != nil {
		f.t.Fatalf("go-inject %v failed: %v\n%s", args, err, output)
	}
	return output
}

func (f *fixture) fails(reason string, args ...string) {
	f.t.Helper()
	output, err := invoke(f.dir, f.env, 2*time.Minute, args...)
	if err == nil {
		f.t.Fatalf("go-inject %v unexpectedly succeeded:\n%s", args, output)
	}
	if !strings.Contains(output, reason) {
		f.t.Fatalf("go-inject %v: want error containing %q, got %v\n%s", args, reason, err, output)
	}
}

func (f *fixture) goCommand(args ...string) string {
	f.t.Helper()
	output, err := command(f.dir, f.env, 2*time.Minute, toolchainGo, args...)
	if err != nil {
		f.t.Fatalf("go %v failed: %v\n%s", args, err, output)
	}
	return output
}

func (f *fixture) run(name, want string) {
	f.t.Helper()
	output, err := command(f.dir, f.env, 30*time.Second, filepath.Join(f.dir, name))
	if err != nil || strings.TrimSpace(output) != want {
		f.t.Fatalf("run %s: got %q (%v), want %q", name, output, err, want)
	}
}

type report struct {
	Session     string
	Fingerprint string
	Rules       []struct{ Rule, Target, State, Version string }
	Packages    []struct {
		Package string
		Result  struct {
			Matches []struct {
				Rule, Target, Function string
				Order                  int
			}
			Links []struct{ Symbol, Signature string }
		}
	}
}

func (f *fixture) inspect(buildOutput string) report {
	f.t.Helper()
	var result report
	if err := json.Unmarshal([]byte(f.cli("inspect", "--json")), &result); err != nil {
		f.t.Fatal(err)
	}
	directory := result.Session
	if directory == "" {
		f.t.Fatal("native inspection has no session path")
	}
	// Retained sessions are CLI-owned temporary directories. Cleanup only the
	// exact path returned by this invocation after verifying its parent and name.
	if filepath.Clean(filepath.Dir(directory)) == filepath.Clean(os.TempDir()) && strings.HasPrefix(filepath.Base(directory), "go-inject-") {
		f.t.Cleanup(func() { _ = os.RemoveAll(directory) })
	}
	if err := json.Unmarshal([]byte(f.cli("inspect", "--json", directory)), &result); err != nil {
		f.t.Fatal(err)
	}
	if result.Fingerprint == "" {
		f.t.Fatal("inspection report has no fingerprint")
	}
	return result
}

// All behavioral tests exercise the public native Go integration, including
// cold/hot builds, source files, tests, runtime hooks and new dependency closure.
func invoke(dir string, env map[string]string, timeout time.Duration, args ...string) (string, error) {
	if len(args) > 0 && (args[0] == "build" || args[0] == "test") {
		goArgs := []string{args[0], `-toolexec="` + cliBinary + `"`}
		goArgs = append(goArgs, args[1:]...)
		return command(dir, env, timeout, toolchainGo, goArgs...)
	}
	return command(dir, env, timeout, cliBinary, args...)
}

const registration = `//go:build goinject

package main
import _ "example.test/app/hooks"
`
