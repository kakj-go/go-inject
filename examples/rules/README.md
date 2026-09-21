# Reusable rules

[简体中文](README_CN.md)

Module: `github.com/kakj-go/go-inject/examples/rules`

```sh
go get github.com/kakj-go/go-inject/examples/rules@v0.1.0-beta.3
```

Enable packages through a `//go:build goinject` registration file in the application entry:

| Import suffix | Selection |
|---|---|
| `/http` | `net/http.(*Client).Do` request/response rule |
| `/gin` | The Gin request variant matching the target version |
| `/all` | Both HTTP and Gin |

HTTP changes a header and returned status as a visible demonstration; it is not a production tracing integration. Gin demonstrates a private method and field, an added atomic field, helper injection, and order `10`.

## Variants

| Package | ID | Target version |
|---|---|---|
| `/gin/v111` | `gin-request` | `>=v1.11.0 <v1.12.0` |
| `/gin/v112` | `gin-request` | `>=v1.12.0 <v1.13.0` |

Both candidates belong to the same provider module and target package. The aggregate imports both, and exactly one is selected for an applicable target. Go module minimum version selection determines the actual Gin version; the consumer may use 1.12 even though this module's minimum dependency is 1.11. Test baselines are `v1.11.0` and `v1.12.0`.

The aggregate packages include ordinary `doc.go` files so they remain valid packages outside the registration build tag. Their tagged imports are metadata for discovery, not runtime initialization.

## Development and release

Consumers in this checkout use `replace ... => ../rules`. Edit these templates and rebuild a consumer to test the changes. The [example runner](../README.md#run-automated-checks) validates the generated behavior.

The nested module release tag is `examples/rules/v0.1.0-beta.3`. The dependency version used in `go.mod` is `v0.1.0-beta.3`. The engine and this reusable module have separate module identities and release tags.
