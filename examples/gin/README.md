# Gin and vendor

[简体中文](README_CN.md) · [All examples](../README.md)

This example serves two real HTTP requests through Gin and combines a reusable rule with a [local order-20 rule](inject/order/gin.go).

The reusable rule intercepts private `Engine.handleHTTPRequest`, reads private `maxParams`, adds an `atomic.Uint64` request counter, and adds a helper that writes response headers. Its order is `10`, so the response records `10,20`. Two requests prove that the added field persists on the engine.

```sh
go mod tidy
go test -toolexec="go-inject" .
go build -toolexec="go-inject" -work -o gin-example .
./gin-example
```

Windows: use `-o gin-example.exe` and `./gin-example.exe`.

Expected output:

```text
PASS gin: private method, private field, added field, helper, order, variant
```

## Native Go with vendor

```sh
go generate .
go test -mod=vendor .
go build -mod=vendor -o gin-example .
./gin-example
go-inject vendor --restore
```

Open `vendor/github.com/gin-gonic/gin` to see the actual generated source. Repeating `go generate .` must not duplicate injection. `--restore` restores the recorded original vendor state and refuses to discard subsequent user edits.

## Version variants

The checked-in module selects Gin `v1.11.0`. To exercise the other supported baseline:

```sh
go get github.com/gin-gonic/gin@v1.12.0
go mod tidy
go test -toolexec="go-inject" .
```

Restore any generated vendor tree before changing dependency versions. The aggregate selects exactly one of [v111](../rules/gin/v111/gin.go) and [v112](../rules/gin/v112/gin.go), based on the target module version. The test reads `gin.Version` and checks the selected variant's header.

For disposable checks of both baselines, run the repository's [example runner](../README.md#run-automated-checks).
