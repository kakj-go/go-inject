# Examples

[简体中文](README_CN.md)

Each application is a separate Go module. Install [go-inject](../README.md#install), enter an example directory, run `go mod tidy`, then use its commands below.

| Example | Shows | Backend |
|---|---|---|
| [basic](basic/README.md) | Local rules, arguments, result/defer, early return, main init | build/test |
| [http](http/README.md) | Real outgoing HTTP, headers, returned response, body preservation | build/test |
| [gin](gin/README.md) | Private method/field, new field, helper, ordered composition | build/test/vendor |
| [external](external/README.md) | Separate rule module, aggregate, deduplication, version variants | build/test |
| [rules](rules/README.md) | Reusable HTTP/Gin rules and aggregates | Library |

Gin examples support `v1.11.0` and `v1.12.0`; the two source variants use disjoint minor-version ranges. HTTP services start through `httptest` on automatically assigned ports and close on completion. No external server is needed.

The consumers use a local `replace` to `../rules` so the checkout is immediately editable. The module path remains `github.com/kakj-go/go-inject/examples/rules`. For a published-module consumer, remove that replace and require `v0.1.0-beta.2`.

## Run automated checks

From the repository root:

```sh
go build -o go-inject ./cmd/go-inject
python examples/check.py --tool ./go-inject
python examples/check.py gin external --gin-version v1.12.0 --tool ./go-inject
```

On Windows, build and pass `./go-inject.exe`. `GOINJECT_BINARY` is an alternative to `--tool`. The Python 3.10+ runner only uses the standard library. It operates on disposable copies, checks binaries and tests, exercises Gin vendor generation/restoration, and checks rejection of standard-library vendor rules. Failures retain their workspace; `--keep-work` does so after success too.

The tests assert injected behavior and should be run with `go test -toolexec="go-inject" .`. Ordinary `go test .` deliberately lacks those effects, except after Gin source has been generated into vendor. Root `go test ./...` does not include nested modules.
