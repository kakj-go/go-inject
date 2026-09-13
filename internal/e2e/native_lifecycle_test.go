package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	proc "github.com/shirou/gopsutil/v4/process"
)

func TestNativeDaemonDoesNotHoldApplicationDirectory(t *testing.T) {
	f := newFixture(t, map[string]string{
		"main.go":       "package main\nfunc Value()int{return 1}\nfunc main(){}\n",
		"inject.go":     registration,
		"hooks/rule.go": "//inject:example.test/app/main.go\npackage hooks\nfunc Value()(r int){defer func(){r++}();return 0}\n",
		"main_test.go": `package main
import("os";"strconv";"testing";"time")
func TestHeld(t *testing.T){
 if Value()!=2{t.Fatal("missing injection")}
 if err:=os.WriteFile("ready",[]byte(strconv.Itoa(os.Getppid())),0600);err!=nil{t.Fatal(err)}
 for {if _,err:=os.Stat("release");err==nil{return};time.Sleep(10*time.Millisecond)}
}
`,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	cmd := exec.CommandContext(ctx, toolchainGo, "test", `-toolexec="`+cliBinary+`"`, "-work", "-count=1", ".")
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = f.dir
	cmd.Env = environment(f.env)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	done := make(chan error, 1)
	// Use a plain Go process, as a user's shell does. A test-owned Windows job
	// would kill the background server at Go exit and hide lifecycle races.
	go func() { done <- cmd.Run() }()
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(f.dir, "release"), nil, 0600)
		cancel()
		<-done
	})
	var parent []byte
	for {
		var err error
		parent, err = os.ReadFile(filepath.Join(f.dir, "ready"))
		if err == nil {
			break
		}
		select {
		case err := <-done:
			done <- err
			t.Fatalf("native test exited before ready: %v\n%s", err, output.String())
		case <-ctx.Done():
			t.Fatal("native test did not reach ready state")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.ParseInt(string(parent), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := proc.NewProcess(int32(pid))
	if err != nil {
		t.Fatal(err)
	}
	started, err := owner.CreateTime()
	if err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(cache, "go-inject", "native", fmt.Sprintf("%d-%d-*", pid, started), "launch.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("native daemon launch for parent %s: %v (%v)", parent, matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var launch struct{ PID int32 }
	if err := json.Unmarshal(data, &launch); err != nil {
		t.Fatal(err)
	}
	daemon, err := proc.NewProcess(launch.PID)
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := daemon.Cwd()
	if err != nil {
		t.Fatal(err)
	}
	actual, err := os.Stat(cwd)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(filepath.Dir(matches[0]))
	if err != nil || !os.SameFile(actual, want) {
		t.Fatalf("daemon still holds a business directory: cwd=%s, want its cache directory (%v)", cwd, err)
	}
	inspection := f.inspect("")
	session := inspection.Session
	if filepath.Clean(filepath.Dir(session)) != filepath.Clean(os.TempDir()) || !strings.HasPrefix(filepath.Base(session), "go-inject-") {
		t.Fatalf("unexpected test session path: %s", session)
	}
	// All compiler actions are finished while TestHeld keeps Go alive. Removing
	// this owned session now makes any late server writes deterministic.
	if err := os.RemoveAll(session); err != nil {
		t.Fatal(err)
	}
	f.write("release", "")
	err = <-done
	done <- err
	if err != nil || !strings.Contains(output.String(), "ok") {
		t.Fatalf("native test failed: %v\n%s", err, output.String())
	}
	// This fixture is owned by t.TempDir. A completed Go command must allow its
	// caller to remove the application immediately, including on Windows.
	if err := os.RemoveAll(f.dir); err != nil {
		t.Fatalf("application remains locked after go test: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		running, _ := daemon.IsRunning()
		states, _ := daemon.Status()
		if !running || strings.Contains(strings.Join(states, ","), proc.Zombie) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native daemon did not exit after Go completed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(session); !os.IsNotExist(err) {
		t.Fatalf("daemon recreated a removed session after Go completed: %v", err)
	}
}
