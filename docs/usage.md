# Usage

[简体中文](usage_CN.md) · [README](../README.md)

## Build and test

```sh
go-inject build [Go build flags] [packages]
go-inject test [Go test flags] [packages]
```

Run commands from your Go project. Use the targets and flags you would pass to Go:

```sh
go-inject build -o server ./cmd/server
go-inject build -tags enterprise ./cmd/server
go-inject test -count=1 ./cmd/server
go-inject test ./...
```

Each selected package is an entry for rule selection. A registration file in `cmd/server` applies to that server and its dependencies. It does not automatically select rules for the separate tests of every library. Add a registration file to a library package when testing that library with injection.

Use ordinary module settings for private dependencies and local development. `replace` and `go.work` identify local source; checksums and versions stay in Go's module files. Registration files use `//go:build goinject`, so `go mod tidy` retains their imports while normal application compilation excludes them. Do not add `goinject` to the final application's build tags yourself.

## Registration and aggregation

An entry's registration file contains static imports:

```go
//go:build goinject

package main

import (
    _ "example.com/app/inject/local"
    _ "example.com/team/rules/all"
)
```

An aggregate uses the same tagged-file format to import child rule packages. Repeated discovery of the same rule is deduplicated. Ordinary imports inside templates and helper libraries supply code dependencies; they do not enable additional rules. The [external example](../examples/external/README.md) demonstrates both aggregation and deduplication.

## Retain and inspect a build

```sh
go-inject build -work -o server ./cmd/server
go-inject inspect --json <session-directory>
```

`-work` keeps the session and prints its path. Inspection reads the saved result, including source locations and generated files. This is the source prepared for that build, not a separately computed preview. Keep the directory while investigating a failure; remove it when finished.

`go-inject version` prints the installed version. Include it, `go version`, the command, and relevant inspection output when reporting a problem.

## Generate vendor source

```sh
go-inject vendor ./cmd/server
go build -mod=vendor -o server ./cmd/server
go test -mod=vendor ./cmd/server
go-inject vendor --restore
```

Vendor mode changes supported third-party dependency source on disk. It records original state so `--restore` can undo tool-owned changes. Existing user edits and subsequent edits to generated files must be resolved explicitly. Repeating the same generation is idempotent.

One physical vendor tree contains one result. Entries requiring different generated versions of a shared package are rejected together. Use separate project copies when both results must exist simultaneously. Plain `go build -mod=vendor` uses the files currently on disk and does not select a fresh set of rules.

Application-module targets, main initialization, standard-library targets, and runtime targets require `build` or `test`; vendor rejects them. The [Gin example](../examples/gin/README.md) demonstrates native Go compilation and restoration for third-party dependencies.

The tool owns `-toolexec`. Multiple entries cannot share one `-o` or profile output path; run those entries separately. User overlays and supported Go build/test flags are passed through the build context.

## Read diagnostics

| Result | Meaning | Next step |
|---|---|---|
| Not applicable | The entry does not depend on the target | Check the entry and registration imports |
| Missing target | The package exists but the file/function/type does not | Update the rule for the dependency version |
| Signature/projection mismatch | The template describes a different API or field | Compare expected and actual declarations |
| No version variant | No implementation supports the actual version | Use a rule version supporting that dependency |
| Multiple variants | More than one implementation applies | Make version ranges or constraints disjoint |
| Declaration conflict | An added declaration or field collides | Rename it or remove the conflicting rule |
| Dependency cycle | New imports would create a cycle | Keep target-specific helpers in the target package |
| Vendor conflict | Entries or local edits cannot share the output | Resolve edits or use separate directories |

Applicable mismatches fail the operation. Go compilation alone does not prove the requested injection occurred.

## Editor setup

Templates are Go source, but projections describe another package and version variants describe alternative targets. The tool's validation in the actual target is authoritative. Configure the editor to show `goinject` files when navigating registration imports; keep that tag out of ordinary application builds.
