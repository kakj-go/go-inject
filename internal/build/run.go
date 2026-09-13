package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kakj-go/go-inject/internal/process"
	"github.com/kakj-go/go-inject/internal/project"
)

func Run(ctx context.Context, dir, kind string, args []string) error {
	dir, flags, patterns, e := project.ResolveArgs(ctx, dir, args)
	if e != nil {
		return e
	}
	env, e := project.Environment(ctx, dir)
	if e != nil {
		return e
	}
	rootFlags := project.ListFlags(flags)
	// Restoring the baseline before discovery prevents old vendor injections
	// from becoming business dependencies of the next entry.
	var cleanup string
	if hasVendorState(env) {
		cleanup, e = os.MkdirTemp("", "go-inject-roots-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(cleanup)
		overlay, e := baselineOverlay(env, cleanup)
		if e != nil {
			return e
		}
		if len(overlay) > 0 {
			p := filepath.Join(cleanup, "overlay.json")
			if e = project.OverlayFile(p, overlay); e != nil {
				return e
			}
			rootFlags = append(project.Remove(rootFlags, "overlay", true), "-overlay", p)
		}
	}
	roots, e := project.List(ctx, env.Dir, rootFlags, false, false, patterns...)
	if e != nil {
		return e
	}
	if len(roots) == 0 {
		return fmt.Errorf("no entry packages matched")
	}
	if len(roots) > 1 {
		for _, flag := range []string{"o", "coverprofile", "cpuprofile", "memprofile", "blockprofile", "mutexprofile", "trace", "outputdir"} {
			if project.Has(flags, flag) {
				return fmt.Errorf("-%s has ambiguous shared output for multiple entry packages; run one entry at a time", flag)
			}
		}
	}
	for _, root := range roots {
		if root.Error != nil {
			return fmt.Errorf("entry %s: %s", root.ImportPath, root.Error.Err)
		}
		if e = runOne(ctx, env, root, flags, kind); e != nil {
			return e
		}
	}
	return nil
}

func runOne(ctx context.Context, env project.Env, root *project.Package, flags []string, kind string) error {
	s, e := Prepare(ctx, env, root, flags, kind)
	if e != nil {
		return e
	}
	keep := project.Has(flags, "work")
	defer func() {
		if keep {
			fmt.Fprintf(os.Stderr, "GOINJECT_WORK=%s\n", s.Dir)
		} else {
			_ = os.RemoveAll(s.Dir)
		}
	}()
	stop, e := StartCoordinator(ctx, s)
	if e != nil {
		keep = true
		return e
	}
	defer stop()
	buildFlags := project.Remove(project.Remove(flags, "overlay", true), "work", false)
	// Recover actual generated inputs when snapshots were evicted independently of GOCACHE.
	if !s.SnapshotValid() && !project.Has(buildFlags, "a") {
		buildFlags = append(buildFlags, "-a")
	}
	// Go's -args belongs after package patterns.
	var tail []string
	for i, a := range buildFlags {
		if a == "-args" {
			tail = append([]string{}, buildFlags[i:]...)
			buildFlags = buildFlags[:i]
			break
		}
	}
	goArgs := []string{kind}
	goArgs = append(goArgs, buildFlags...)
	goArgs = append(goArgs, "-overlay", filepath.Join(s.Dir, "overlay.json"), "-toolexec="+quoteTool(s.Executable), root.Base())
	goArgs = append(goArgs, tail...)
	c := project.Command(ctx, env.Dir, goArgs...)
	c.Env = append(c.Environ(), SessionEnv+"="+filepath.Join(s.Dir, "session.json"))
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	e = process.Run(ctx, c)
	finishErr := s.Finish()
	if e != nil {
		keep = true
		return fmt.Errorf("entry %s: %w", root.Base(), e)
	}
	if finishErr != nil {
		keep = true
		return finishErr
	}
	return s.Complete()
}
