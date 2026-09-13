// go-inject applies ordinary Go interceptor packages during builds or to vendor.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime/debug"
	"syscall"

	engine "github.com/kakj-go/go-inject/internal/build"
)

var version = "devel"
var commit = "unknown"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "go-inject:", err)
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() > 0 {
			os.Exit(exit.ExitCode())
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	dir, e := os.Getwd()
	if e != nil {
		return e
	}
	switch args[0] {
	case "build", "test":
		return engine.Run(ctx, dir, args[0], args[1:])
	case "vendor":
		return engine.Vendor(ctx, dir, args[1:])
	case "inspect":
		f := flag.NewFlagSet("inspect", flag.ContinueOnError)
		asJSON := f.Bool("json", false, "print the complete JSON report")
		if e = f.Parse(args[1:]); e != nil {
			return e
		}
		if f.NArg() != 1 {
			return fmt.Errorf("usage: go-inject inspect [--json] <session-directory>")
		}
		return engine.Inspect(f.Arg(0), *asJSON)
	case "version":
		v := version
		if v == "devel" {
			if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
				v = info.Main.Version
			}
		}
		fmt.Printf("go-inject %s (%s)\n", v, commit)
		return nil
	case "__toolexec":
		return engine.Worker(ctx, args[1:])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
func usage() {
	fmt.Println(`go-inject — ordinary Go code injection

Usage:
  go-inject build [go build flags] [packages]
  go-inject test [go test flags] [packages]
  go-inject vendor [packages]
  go-inject vendor --restore
  go-inject inspect [--json] <session-directory>
  go-inject version

Enable interceptor packages with blank imports in a //go:build goinject file.
Use build/test -work to retain actual generated source and the inspection report.`)
}
