# External module and aggregation

[简体中文](README_CN.md) · [All examples](../README.md)

[inject.go](inject.go) imports `rules/all` and `rules/http` from the separate `github.com/kakj-go/go-inject/examples/rules` module. `all` already includes HTTP. The request and response must contain exactly one injected header, proving that the direct and aggregated import do not apply the rule twice.

The application uses Gin `v1.12.0`. The Gin aggregate imports two candidates with the same explicit ID and provider module; version constraints select the 1.12 implementation. A real local HTTP request verifies both the Gin entry rule and the standard-library client rule while preserving the body.

```sh
go mod tidy
go-inject test .
go-inject build -work -o external-example .
./external-example
```

Windows: use `-o external-example.exe` and `./external-example.exe`.

Expected output:

```text
PASS external: module rules, aggregate, deduplication, selected Gin variant
```

The local `replace => ../rules` is for developing this checkout. To consume the published module, remove that replacement and run:

```sh
go get github.com/kakj-go/go-inject/examples/rules@v0.1.0-beta.1
go mod tidy
```

The source imports stay the same. Teams can distribute their own rules using the same public/private Go module mechanism.

Vendor mode is rejected for this selection because `rules/all` includes the standard-library HTTP rule. Use only `rules/gin` for the [vendor example](../gin/README.md).
