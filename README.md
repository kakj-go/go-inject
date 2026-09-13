# go-inject

[简体中文](README_CN.md)

Write an ordinary Go function to intercept another Go function. Enable rules with imports, then build an instrumented binary or generate inspectable source in `vendor`.

**beta-0.1** · Go 1.26 or newer · [Rule reference](docs/rules.md) · [Examples](examples/README.md)

## Install

```sh
go install github.com/kakj-go/go-inject/cmd/go-inject@v0.1.0-beta.1
go-inject version
```

For a checkout, use `go build -o go-inject ./cmd/go-inject` (`go-inject.exe` on Windows).

## A small example

Suppose `example.com/app/price/price.go` contains:

```go
package price

func Quote(units int) int {
    return units * 10
}
```

Create `inject/price/price.go` in that application:

```go
//inject:example.com/app/price/price.go
package price

func Quote(units int) (total int) {
    if units < 0 {
        return 0
    }
    units++
    defer func() { total += 5 }()
    return 0
}
```

The final top-level `return` is a placeholder. go-inject removes it and places the original function body after the template. The conditional return remains an early return; the deferred function can read and change the final result. `Quote(2)` becomes `35`, and `Quote(-1)` becomes `0`.

Enable the rule in the application's entry package, for example `cmd/server/inject.go`:

```go
//go:build goinject

package main

import _ "example.com/app/inject/price"
```

Build or test with normal Go flags and targets:

```sh
go-inject build -o server ./cmd/server
go-inject test ./cmd/server
```

Registration imports select rules for that entry. They are read statically; the final application build does not enable the `goinject` tag. Put a registration file in a library's own package when its tests need rules. See the runnable [basic example](examples/basic/README.md).

## Use and share rules

Rules can live in your project, a public module, or a private module. Use `go.mod`, `go.sum`, `replace`, and `go.work` to manage them. A tagged registration file can also aggregate several rule packages. No runtime registration is required.

```go
//go:build goinject

package main

import _ "github.com/kakj-go/go-inject/examples/rules/http"
```

The [reusable example module](examples/rules/README.md) includes HTTP and Gin rules, aggregation, and version variants. The [HTTP example](examples/http/README.md) changes an outgoing request header and response status while preserving the response body. The [Gin example](examples/gin/README.md) uses a private method and field, adds a field and helper, and composes two ordered rules.

## Inspect the actual code

```sh
go-inject build -work -o server ./cmd/server
go-inject inspect --json <session-directory>
```

`-work` retains the build session and prints its location. Inspect that session to review the selected rules, target versions, matches, and generated files used by the build.

For third-party dependencies, generate source directly into `vendor`:

```sh
go-inject vendor ./cmd/server
go build -mod=vendor -o server ./cmd/server
go-inject vendor --restore
```

A vendor tree represents one generated result. Conflicting entry selections and local edits are reported; repeated generation must not inject the same source again. Vendor supports third-party dependency rules. Application-module, main-initialization, standard-library, and `runtime` targets use `build` or `test` instead.

## Contract

- A selected target outside the entry's business dependencies is not applicable.
- An applicable target with a missing function, incompatible signature, missing projected field, or unsupported version fails the operation.
- Multiple rules run in ascending `//inject:order` order, then stable identity order. The default order is `0`; deferred calls retain Go's reverse execution order.
- Same-named types are projections. Use `//inject:add` for new fields, helpers, types, variables, constants, or initialization.
- Version variants sharing provider module, explicit rule ID, and target package select exactly one applicable implementation.

The [rule reference](docs/rules.md) defines these semantics. The [usage guide](docs/usage.md) covers commands and troubleshooting. The [architecture](docs/architecture.md) explains package loading, transformation, dependencies, and the two backends.

go-inject is a general injection tool. Tracing runtimes, context propagation, sampling, and exporters belong to the rules and runtime libraries built on top of it.

## Development

```sh
go test ./...
go build -o go-inject ./cmd/go-inject
python examples/check.py --tool ./go-inject
```

Use `./go-inject.exe` on Windows. Example validation runs in temporary copies and starts its HTTP servers on automatically assigned local ports. See [testing](docs/testing.md), [contributing](CONTRIBUTING.md), and the [changelog](CHANGELOG.md).

## Acknowledgments

The project builds on the mechanisms and experience demonstrated by [go-build-hijacking](https://github.com/0x2E/go-build-hijacking), [Apache SkyWalking Go](https://github.com/apache/skywalking-go), [Orchestrion](https://github.com/DataDog/orchestrion), and [Garble](https://github.com/burrowers/garble). See the architecture document for the implementation boundaries informed by these projects.

Licensed under [Apache-2.0](LICENSE).
