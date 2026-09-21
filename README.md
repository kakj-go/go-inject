# go-inject

[简体中文](README_CN.md)

Intercept Go functions with ordinary Go templates. Use the native Go toolchain to inject during compilation, or generate readable dependency source in vendor.

**Go 1.25 / 1.26 / 1.27** · [Usage](docs/usage.md) · [Rules](docs/rules.md) · [Examples](examples/README.md)

## Install

Install the **beta-0.3** prerelease with Go 1.25, 1.26, or 1.27:

```sh
go install github.com/kakj-go/go-inject/cmd/go-inject@v0.1.0-beta.3
go-inject version
```

Put `GOBIN` (or the `bin` directory under `go env GOPATH`) on `PATH`. No application runtime registration or central plugin service is required.

## Write and select rules

A template names a target file, then declares the function to intercept. This is the [basic example](examples/basic/inject/quote/quote.go):

```go
//inject:github.com/kakj-go/go-inject/examples/basic/main.go
package quote

func Quote(units int) (total int) {
    if units < 0 {
        return 0
    }
    units++
    defer func() { total += 5 }()
    return 0
}
```

The final top-level return is a placeholder. Conditional returns and deferred calls keep their Go semantics. With an original body returning `units * 10`, `Quote(2)` becomes `35` and `Quote(-1)` becomes `0`.

Enable local, external, or aggregate rule packages with imports in the entry package. A vendor-capable example uses this registration file:

```go
//go:build goinject || generate

package main

//go:generate go-inject vendor .

import (
    _ "github.com/kakj-go/go-inject/examples/gin/inject/order"
    _ "github.com/kakj-go/go-inject/examples/rules/gin"
)
```

Manage rule versions with normal `go.mod`, `go.sum`, `go get`, `replace`, and `go.work`. The registration tag is excluded from business builds. Go automatically enables `generate` while running generators.

## 1. Compile-time injection

```sh
go build -a -toolexec="go-inject" .
go test -toolexec="go-inject" .
```

Go owns the build and package scheduling. go-inject receives the compiler inputs, prepares dependencies, and supplies generated Go source. It can target the application, dependencies, the standard library, and runtime Go implementations participating in the build. Original source files are not edited.

`-a` requests a full rebuild. Normal builds may omit it: changes to effective rules and source invalidate the toolchain cache through the tool fingerprint.

```sh
go build -work -toolexec="go-inject" .
go-inject inspect --json
```

Inspection selects the latest retained session in the current directory, including actual source snapshots on cache hits. An explicit session directory can also be supplied. This diagnostic command does not replace the native build entry point.

Run the [basic](examples/basic/README.md), [HTTP](examples/http/README.md), or [Gin](examples/gin/README.md) examples to see the behavior.

## 2. Generate vendor source

For rules targeting vendored dependencies, use the registration directive above:

```sh
go generate .
go build -mod=vendor .
go test -mod=vendor .
```

The generator creates vendor if needed, type-checks the planned result, and delivers changes transactionally. Existing vendor patches become the baseline. Repeating generation starts from that baseline; edits to managed generated files are reported as conflicts.

Commit `vendor` together with `.goinject/vendor-state`. The completed state contains relative file identities, baselines, output hashes, rule ownership, and the selected plan, so another checkout can generate or restore it. To remove tool changes, run `go-inject vendor --restore`.

Go build/test do not automatically run generate. Run it again after changing rules, dependency versions, or relevant platform/build settings. See the runnable [Gin vendor example](examples/gin/README.md).

### What generate cannot inject

| Target | Compile-time mode | Vendor generation |
|---|---|---|
| Application and workspace main modules | Yes | Rejected |
| Third-party dependency source covered by vendor | Yes | Yes |
| Standard library, including net/http and database/sql | Yes | Rejected |
| runtime structures and Go implementation hooks | Yes | Rejected |
| Initialization routed to application main/testmain | Yes | Rejected |
| An explicit init added to a vendored package | Yes | Yes |

A local replacement dependency may be generated when Go includes it in vendor. Arbitrary files outside the active package graph, assembly-only/compiler-intrinsic functions without a Go body, and implicit import-cycle bridges are not supported. Native bridges use verified nongeneric functions. A selected unsupported target fails instead of being silently skipped.

One vendor tree represents one plan. In a native multi-entry build/test, different generated source for a shared dependency is also an error; build those entries separately. Compatible selections may share Go's compilation actions.

## Building tracing integrations

This tool supplies injection mechanics: private function/field access, argument and result changes, explicit additions, initialization, runtime hooks, and function bridges. Tracer state, propagation protocols, sampling, exporters, metrics, logging, and profiling belong in integration modules such as [go-inject-trace-contrib](https://github.com/kakj-go/go-inject-trace-contrib).

Compile-time mode is the foundation for a full SkyWalking-style integration. Vendor mode supports the covered libraries but cannot provide equivalent automatic standard-library/runtime coverage. Matching an entire agent requires independent protocol, lifecycle, compatibility, and backend E2E validation; translating interception points alone is insufficient. See [integration boundaries](docs/integrations.md).

## Development and license

```sh
go test -timeout=30m ./...
go build -o bin/ ./cmd/go-inject
python examples/check.py --tool bin/go-inject
```

On Windows use `bin/go-inject.exe`. Tests use temporary projects and local servers with automatic ports. See [testing](docs/testing.md), [contributing](CONTRIBUTING.md), and [changes](CHANGELOG.md).

The project draws on [Orchestrion](https://github.com/DataDog/orchestrion), [SkyWalking Go](https://github.com/apache/skywalking-go), [go-build-hijacking](https://github.com/0x2E/go-build-hijacking), and [Garble](https://github.com/burrowers/garble). Licensed under [Apache-2.0](LICENSE); distributions include [third-party notices](THIRD_PARTY_LICENSES.txt).
