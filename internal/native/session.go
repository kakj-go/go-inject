package native

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	engine "github.com/kakj-go/go-inject/internal/build"
	"github.com/kakj-go/go-inject/internal/filelock"
	"github.com/kakj-go/go-inject/internal/project"
)

type bundle struct {
	Fingerprint, Error string
	Sessions           []string
}
type launch struct {
	Invocation invocation
	PID        int32
	Started    int64
}
type epoch struct {
	PID     int32
	Started int64
	Value   string
}

func writeJSON(name string, value any) error {
	data, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(name), ".write-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(tmp, name)
}
func readJSON(name string, value any) error {
	b, e := os.ReadFile(name)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, value)
}

func ensure(ctx context.Context) (bundle, error) {
	i, e := discover(ctx)
	if e != nil {
		return bundle{}, e
	}
	cache, e := os.UserCacheDir()
	if e != nil {
		return bundle{}, e
	}
	exe, e := os.Executable()
	if e != nil {
		return bundle{}, e
	}
	dir := filepath.Join(cache, "go-inject", "native", fmt.Sprintf("%d-%d-%s", i.PID, i.Started, digest([]byte(exe))[:16]))
	if e = os.MkdirAll(dir, 0700); e != nil {
		return bundle{}, e
	}
	unlock, e := filelock.Lock(filepath.Join(dir, "start.lock"))
	if e != nil {
		return bundle{}, e
	}
	var ready bundle
	if e = readJSON(filepath.Join(dir, "ready.json"), &ready); e == nil {
		unlock()
		return ready, bundleError(ready)
	}
	var started launch
	active := readJSON(filepath.Join(dir, "launch.json"), &started) == nil && alive(started.PID, started.Started)
	if !active {
		if e = writeJSON(filepath.Join(dir, "invocation.json"), i); e != nil {
			unlock()
			return bundle{}, e
		}
		log, e := os.OpenFile(filepath.Join(dir, "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			unlock()
			return bundle{}, e
		}
		cmd := exec.Command(exe, "__serve", dir)
		cmd.Stdin = nil
		cmd.Stdout = log
		cmd.Stderr = log
		background(cmd)
		e = cmd.Start()
		log.Close()
		if e != nil {
			unlock()
			return bundle{}, e
		}
		pid := int32(cmd.Process.Pid)
		p, e := processTime(pid)
		if e != nil {
			_ = cmd.Process.Kill()
			unlock()
			return bundle{}, e
		}
		_ = cmd.Process.Release()
		started = launch{Invocation: i, PID: pid, Started: p}
		e = writeJSON(filepath.Join(dir, "launch.json"), started)
		if e != nil {
			unlock()
			return bundle{}, e
		}
	}
	unlock()
	deadline := time.NewTimer(3 * time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if e = readJSON(filepath.Join(dir, "ready.json"), &ready); e == nil {
			if e = bundleError(ready); e != nil {
				return ready, e
			}
			return ready, nil
		}
		select {
		case <-ctx.Done():
			return ready, ctx.Err()
		case <-deadline.C:
			return ready, fmt.Errorf("native session startup timed out; inspect %s", filepath.Join(dir, "daemon.log"))
		case <-ticker.C:
			if !alive(started.PID, started.Started) {
				log, _ := os.ReadFile(filepath.Join(dir, "daemon.log"))
				if len(log) > 16384 {
					log = log[len(log)-16384:]
				}
				return ready, fmt.Errorf("native session stopped before initialization: %s", strings.TrimSpace(string(log)))
			}
		}
	}
}
func bundleError(b bundle) error {
	if b.Error != "" {
		return fmt.Errorf("native build plan: %s", b.Error)
	}
	return nil
}

func cacheEpoch(s *engine.Session, i invocation) (string, error) {
	unlock, e := filelock.Lock(filepath.Join(s.Cache, "native.lock"))
	if e != nil {
		return "", e
	}
	defer unlock()
	name := filepath.Join(s.Cache, "native-epoch.json")
	var old epoch
	if readJSON(name, &old) == nil && (s.SnapshotValid() || alive(old.PID, old.Started)) {
		return old.Value, nil
	}
	value := make([]byte, 16)
	if _, e = rand.Read(value); e != nil {
		return "", e
	}
	old = epoch{PID: i.PID, Started: i.Started, Value: hex.EncodeToString(value)}
	return old.Value, writeJSON(name, old)
}

// Serve lives only as long as its original Go invocation. Workers discover its
// immutable session manifest; newly introduced dependency builds use explicit
// session environments and never recursively discover another parent build.
func Serve(ctx context.Context, dir string) (err error) {
	var i invocation
	if err = readJSON(filepath.Join(dir, "invocation.json"), &i); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if !alive(i.PID, i.Started) {
					cancel()
					return
				}
			}
		}
	}()
	var out bundle
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("native session initialization failed: %v", value)
		}
		if err != nil {
			out.Error = err.Error()
			_ = writeJSON(filepath.Join(dir, "ready.json"), out)
		}
	}()
	env, flags, roots, err := i.resolve(ctx)
	if err != nil {
		return err
	}
	// Use the parent's selected SDK for every child Go invocation, including
	// semantic queries with their own module-cache view.
	if err = os.Setenv("PATH", filepath.Join(env.GOROOT, "bin")+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		return err
	}
	if err = os.Setenv("GOTOOLCHAIN", "local"); err != nil {
		return err
	}
	env.Go = filepath.Join(env.GOROOT, "bin", "go")
	if filepath.Ext(os.Args[0]) == ".exe" {
		env.Go += ".exe"
	}
	kind := i.Kind
	if kind == "install" {
		kind = "build"
	}
	var sessions []*engine.Session
	defer func() {
		for _, s := range sessions {
			if !project.Has(flags, "work") {
				_ = os.RemoveAll(s.Dir)
			}
		}
	}()
	var versions []string
	for _, root := range roots {
		s, e := engine.PrepareNative(ctx, env, root, flags, kind)
		if e != nil {
			return e
		}
		sessions = append(sessions, s)
		ep, e := cacheEpoch(s, i)
		if e != nil {
			return e
		}
		versions = append(versions, s.Fingerprint+":"+ep)
	}
	if err = compatible(sessions); err != nil {
		return err
	}
	sort.Strings(versions)
	out.Fingerprint = digest([]byte(strings.Join(versions, "\n")))
	var stops []func()
	defer func() {
		for _, stop := range stops {
			stop()
		}
	}()
	for _, s := range sessions {
		s.ToolFingerprint = out.Fingerprint
		stop, e := engine.StartCoordinator(ctx, s)
		if e != nil {
			return e
		}
		stops = append(stops, stop)
		if e = s.Finish(); e != nil {
			return e
		}
		out.Sessions = append(out.Sessions, filepath.Join(s.Dir, "session.json"))
	}
	if err = writeJSON(filepath.Join(dir, "ready.json"), out); err != nil {
		return err
	}
	if project.Has(flags, "work") {
		name, e := latestPath(env.Dir)
		if e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(name), 0700); e != nil {
			return e
		}
		if e = writeJSON(name, out); e != nil {
			return e
		}
	}
	<-ctx.Done()
	// A successful compiler/linker already persisted its report and snapshot.
	// Keep failure diagnostics for -work, but never mark a canceled plan complete.
	for _, s := range sessions {
		_ = s.Finish()
	}
	return nil
}

func compatible(sessions []*engine.Session) error {
	fingerprints := map[string]string{}
	owners := map[string]string{}
	for _, s := range sessions {
		changes, e := s.PlannedResults()
		if e != nil {
			return e
		}
		for name := range s.Business {
			p := s.Packages[name]
			if p == nil {
				continue
			}
			sources, e := engine.Sources(p, s.Overlay, s.Names())
			if e != nil {
				return e
			}
			files := map[string][]byte{}
			for _, src := range sources {
				files[filepath.Base(src.Path)] = src.Data
			}
			if r := changes[name]; r != nil {
				for path, data := range r.Replacements {
					files[filepath.Base(path)] = data
				}
				for path, data := range r.Additions {
					if !strings.HasPrefix(filepath.ToSlash(path), "main/") {
						files[path] = data
					}
				}
			}
			data, _ := json.Marshal(files)
			fp := digest(data)
			if old, exists := fingerprints[name]; exists && old != fp {
				return fmt.Errorf("entries %s and %s require different generated source for shared package %s; run separate go build/test commands", owners[name], s.RootPath, name)
			}
			fingerprints[name] = fp
			owners[name] = s.RootPath
		}
	}
	return nil
}

func Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected a Go compiler tool path")
	}
	if os.Getenv(engine.SessionEnv) != "" {
		return engine.Worker(ctx, args)
	}
	b, e := ensure(ctx)
	if e != nil {
		return e
	}
	for _, a := range args[1:] {
		if a == "-V=full" {
			out, e := exec.CommandContext(ctx, args[0], "-V=full").Output()
			if e != nil {
				return e
			}
			fmt.Printf("%s go-inject=%s\n", strings.TrimSpace(string(out)), b.Fingerprint)
			return nil
		}
	}
	pkg := os.Getenv("TOOLEXEC_IMPORTPATH")
	base := strings.Split(pkg, " [")[0]
	var chosen *engine.Session
	for _, name := range b.Sessions {
		s, e := engine.ReadSession(name)
		if e != nil {
			return e
		}
		if base == s.RootPath || base == s.RootPath+".test" || strings.HasSuffix(pkg, " ["+s.RootPath+".test]") {
			chosen = s
			break
		}
		if chosen == nil && s.Packages[base] != nil {
			chosen = s
		}
	}
	if chosen == nil {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	if e = os.Setenv(engine.SessionEnv, filepath.Join(chosen.Dir, "session.json")); e != nil {
		return e
	}
	if err := engine.Worker(ctx, args); err != nil {
		return err
	}
	if strings.TrimSuffix(filepath.Base(args[0]), ".exe") == "compile" {
		for _, name := range b.Sessions {
			s, err := engine.ReadSession(name)
			if err != nil {
				return err
			}
			if s.Dir != chosen.Dir && s.Business[base] {
				if err = s.ReplicateRecord(chosen.Dir, pkg); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func latestPath(dir string) (string, error) {
	cache, e := os.UserCacheDir()
	if e != nil {
		return "", e
	}
	if abs, e := filepath.Abs(dir); e == nil {
		dir = abs
	}
	if real, e := filepath.EvalSymlinks(dir); e == nil {
		dir = real
	}
	return filepath.Join(cache, "go-inject", "latest", digest([]byte(dir))+".json"), nil
}

// LatestSession also works when Go satisfied the whole build from its cache:
// version probes have captured stderr, so they cannot print a work directory.
func LatestSession(dir string) (string, error) {
	name, e := latestPath(dir)
	if e != nil {
		return "", e
	}
	var b bundle
	if e = readJSON(name, &b); e != nil {
		return "", fmt.Errorf("no retained native session for %s; run go build/test -work -toolexec=go-inject first", dir)
	}
	if len(b.Sessions) != 1 {
		return "", fmt.Errorf("multiple retained entries; inspect one session directory explicitly: %v", b.Sessions)
	}
	return filepath.Dir(b.Sessions[0]), nil
}
