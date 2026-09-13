package build

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/kakj-go/go-inject/internal/process"
	"github.com/kakj-go/go-inject/internal/project"
)

type exportRequest struct {
	Package string
	Chain   []string
}
type exportResponse struct {
	Packages []*project.Package
	Error    string
}
type exportJob struct {
	done   chan struct{}
	result exportResponse
}
type coordinator struct {
	session *Session
	ctx     context.Context
	mu      sync.Mutex
	jobs    map[string]*exportJob
	wg      sync.WaitGroup
	closed  bool
}

func quoteTool(p string) string { return `"` + p + `" __toolexec` }
func (s *Session) commandFlags() []string {
	f := project.ListFlags(s.Flags)
	f = project.Remove(f, "overlay", true)
	f = append(f, "-overlay", s.Dir+string(os.PathSeparator)+"overlay.json", "-toolexec="+quoteTool(s.Executable))
	return f
}

func StartCoordinator(ctx context.Context, s *Session) (func(), error) {
	ctx, cancel := context.WithCancel(ctx)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		cancel()
		return nil, e
	}
	secret := make([]byte, 32)
	if _, e = rand.Read(secret); e != nil {
		cancel()
		l.Close()
		return nil, e
	}
	s.Token = hex.EncodeToString(secret)
	s.Endpoint = "http://" + l.Addr().String()
	c := &coordinator{session: s, ctx: ctx, jobs: map[string]*exportJob{}}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/export" || r.Header.Get("Authorization") != "Bearer "+s.Token {
			http.Error(w, "unauthorized", http.StatusForbidden)
			return
		}
		var request exportRequest
		if e := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&request); e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		response := c.resolve(request)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	})}
	if e = s.Save(); e != nil {
		cancel()
		l.Close()
		return nil, e
	}
	go func() { _ = server.Serve(l) }()
	return func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		cancel()
		_ = server.Close()
		c.wg.Wait()
	}, nil
}

func (c *coordinator) resolve(req exportRequest) exportResponse {
	if req.Package == "" || strings.HasPrefix(req.Package, "-") {
		return exportResponse{Error: "invalid package request"}
	}
	for _, p := range req.Chain {
		if p == req.Package {
			return exportResponse{Error: "injected dependency cycle: " + strings.Join(append(req.Chain, req.Package), " -> ")}
		}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return exportResponse{Error: "build session closed"}
	}
	job, exists := c.jobs[req.Package]
	if !exists {
		job = &exportJob{done: make(chan struct{})}
		c.jobs[req.Package] = job
		c.wg.Add(1)
	}
	c.mu.Unlock()
	if exists {
		select {
		case <-job.done:
			return job.result
		case <-c.ctx.Done():
			return exportResponse{Error: c.ctx.Err().Error()}
		}
	}
	defer close(job.done)
	defer c.wg.Done()
	args := []string{"list", "-json", "-deps", "-export"}
	args = append(args, c.session.commandFlags()...)
	args = append(args, "--", req.Package)
	cmd := project.Command(c.ctx, c.session.Env.Dir, args...)
	chain, _ := json.Marshal(append(req.Chain, req.Package))
	cmd.Env = append(cmd.Environ(), SessionEnv+"="+c.session.Dir+string(os.PathSeparator)+"session.json", "GOINJECT_RESOLVE_CHAIN="+string(chain))
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if e := process.Run(c.ctx, cmd); e != nil {
		job.result.Error = fmt.Sprintf("resolve dependency %s: %s: %v", req.Package, strings.TrimSpace(stderr.String()), e)
		return job.result
	}
	d := json.NewDecoder(&out)
	for {
		var p project.Package
		e := d.Decode(&p)
		if e == io.EOF {
			break
		}
		if e != nil {
			job.result.Error = e.Error()
			break
		}
		if p.Error != nil {
			job.result.Error = p.Error.Err
			break
		}
		if p.Export != "" {
			job.result.Packages = append(job.result.Packages, &p)
		}
	}
	return job.result
}

func (s *Session) Exports(ctx context.Context, pkg, caller string) ([]*project.Package, error) {
	var chain []string
	_ = json.Unmarshal([]byte(os.Getenv("GOINJECT_RESOLVE_CHAIN")), &chain)
	if caller != "" && (len(chain) == 0 || chain[len(chain)-1] != caller) {
		chain = append(chain, caller)
	}
	data, _ := json.Marshal(exportRequest{Package: pkg, Chain: chain})
	req, e := http.NewRequestWithContext(ctx, "POST", s.Endpoint+"/export", bytes.NewReader(data))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+s.Token)
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("coordinator status %s", resp.Status)
	}
	var result exportResponse
	if e = json.NewDecoder(resp.Body).Decode(&result); e != nil {
		return nil, e
	}
	if result.Error != "" {
		return nil, fmt.Errorf("%s", result.Error)
	}
	return result.Packages, nil
}
