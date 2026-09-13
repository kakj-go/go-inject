# Basic

[简体中文](README_CN.md) · [All examples](../README.md)

This module changes the application's `Quote` function with a [local rule](inject/quote/quote.go). The rule increments the parameter, adds `5` to the final result in a defer, and returns early for negative input. A [main initializer](inject/quote/startup.go) prints a startup marker.

```sh
go mod tidy
go test -toolexec="go-inject" .
go build -toolexec="go-inject" -work -o basic .
./basic
```

Windows: use `-o basic.exe` and `./basic.exe`.

Expected output:

```text
injection ready
quote=35 early=0
```

Without injection, `Quote(2)` returns `20` and `Quote(-1)` returns `-10`. The rule's final top-level `return 0` is removed; its conditional return remains. The tests also verify zero input. See [inject.go](inject.go) for entry-specific registration.

Run `go-inject inspect --json` in this directory after `-work` to review the latest retained generated code, including cache hits. This example modifies application source in the build session; the third-party source delivery example is [Gin vendor](../gin/README.md).
