# Usage

[简体中文](usage_CN.md) · [README](../README.md)

## Native build and test

```sh
go build -a -toolexec="go-inject" .
go build -toolexec="go-inject" -o server ./cmd/server
go test -toolexec="go-inject" -count=1 ./cmd/server
```

The injector is a compiler proxy, not a replacement Go build driver. Keep ordinary Go flags, module/workspace configuration and package patterns. The proxy discovers its parent Go process, establishes a session, and coordinates imports and archive dependencies. It delegates tools with their original exit status. Compiler intrinsics or functions implemented only in assembly have no ordinary Go body to intercept.

`-a` forces compilation; otherwise the effective rules and source fingerprint integrate with Go's cache. Rule providers are ordinary module dependencies. `go mod tidy`, `go.sum`, `replace` and `go.work` continue to manage their source and versions.

Each entry chooses rules with registration imports. If a shared dependency needs different generated code for two entries in one Go invocation, the operation fails with both entry names; run separate Go commands. Compatible selections share Go's compilation. Library tests need their own registration, rather than inheriting a main package's rules automatically.

## Registration and aggregation

```go
//go:build goinject || generate

package main

//go:generate go-inject vendor .

import (
    _ "example.com/app/inject/local"
    _ "example.com/team/rules/all"
)
```

Go enables `generate` when scanning generators; ordinary application builds enable neither tag. Rule discovery reads these imports without executing template initializers. An aggregate imports child rule packages with the same tagged-file form. Normal helper imports do not activate more rules. Duplicate discovery of the same rule is deduplicated.

## Retain and inspect source

```sh
go build -work -toolexec="go-inject" .
go-inject inspect --json
```

The optional inspector reads the latest retained injection session for the current working directory. The report includes its `Session` directory, effective rules and actual source snapshots. Cached builds retain the corresponding snapshot too. To select an earlier session, supply its directory explicitly. A multi-entry invocation requires choosing an individual session directory.

Go also prints its own `WORK` directory. Compiler version probes capture their stderr, and compiler diagnostics may be replayed from Go's cache, so the injector does not print transient session paths as compiler diagnostics. Use the inspector for the current session.

`go-inject version` prints the tool version. Include it, `go version`, the native Go command and inspection output when reporting a problem.

## Generate vendor

For a vendor-capable selection, the registration directive runs:

```sh
go generate .
go build -mod=vendor .
go test -mod=vendor .
```

Go does not run generate automatically. Run it again after rule, dependency, target-platform or relevant build-setting changes. A generator executes in the source directory of its package; keep one deliberate plan for a shared vendor tree. A generator directive can list several entry packages when their required shared source is identical.

Only dependencies actually covered by vendor may be changed. Application/workspace main modules, standard library, runtime, and main/testmain-routed initialization are rejected. Adding `init` inside a vendored dependency is supported. Local replacement dependencies work when Go includes them in vendor. See [the Gin example](../examples/gin/README.md).

Generation preserves an existing vendor tree as its initial baseline, checks types before delivery, and uses hashes and a process lock to reject changed inputs or edited managed output. Initial creation has its own recoverable directory-delivery transaction. Source positions and completed state use portable paths. Commit `vendor` and `.goinject/vendor-state/manifest.json`; in-progress journals and locks are local operational files.

```sh
go-inject vendor --restore
```

Restore is an explicit maintenance operation: it rechecks ownership hashes before restoring baselines. State format 2 belongs to the native integration; restore beta.1-generated state using its original tool before regenerating with this version. Unknown state formats fail rather than guessing a baseline.

## Diagnostics

| Result | Meaning and response |
|---|---|
| Not applicable | The entry does not depend on the target; inspect its imports |
| Missing file/function/type | Update the rule for the actual dependency source |
| Signature or field mismatch | Real Go types disagree with the descriptor |
| No/overlapping version variants | Supply exactly one matching implementation |
| Declaration conflict | Explicit additions collide with an existing declaration |
| Import cycle | Move shared runtime code out of its instrumented consumers or use an explicit validated function bridge |
| Different shared output | Build the affected entries separately |
| Edited vendor/state | Resolve the edit; generated code is not overwritten silently |
| Parent Go process unavailable | The native session cannot safely determine its build context |

For tracing/runtime integration requirements and vendor limitations, see [integration boundaries](integrations.md).
