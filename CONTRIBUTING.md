# Contributing

[简体中文](CONTRIBUTING_CN.md)

Use Go 1.26 or 1.27. Start with [architecture](docs/architecture.md), the [rule contract](docs/rules.md), and [testing](docs/testing.md).

Before editing, inspect `git status`. Preserve unrelated work. Prefer the simplest implementation satisfying the contract and existing Go dependencies over a new framework. Keep backend source files below 2,000 lines. Changes to architecture boundaries need matching documentation.

## Validate a change

```sh
gofmt -w <changed-go-files>
go test -timeout=30m ./...
go build -o go-inject ./cmd/go-inject
python examples/check.py --tool ./go-inject
```

Use an `.exe` filename on Windows. Nested example modules are intentionally separate; root `go test -timeout=30m ./...` does not run them. Run the real example CLI checks for changes to loading, source rewriting, dependencies, or backends.

Add a regression test for the observable failure. Check runtime results, not just transformed text. Avoid fixed ports, persistent services, external credentials, or mutation of the user's module cache. Temporary fixtures must clean up after success and retain useful diagnostics on failure.

## Submit a change

Describe the problem and resulting behavior. Include the reproduction, relevant validation, and any tests not run. Keep English and Chinese documentation aligned. A public rule example should include its target version range and a self-contained behavior test.

Do not add compatibility aliases for removed experimental interfaces. Do not make a compiler implementation detail part of ordinary user configuration unless it enables a meaningful choice.

## Releases

The current beta is displayed as `beta-0.2` and versioned `v0.1.0-beta.2`. The tool installs from `github.com/kakj-go/go-inject/cmd/go-inject`. The nested reusable rules module is separately tagged `examples/rules/v0.1.0-beta.2`; consumers require `github.com/kakj-go/go-inject/examples/rules v0.1.0-beta.2`.

Update [CHANGELOG.md](CHANGELOG.md), installation examples, and release defaults together. Verify the final master commit with source CI and clean remote installation by SHA. Run the release-artifacts workflow and require all twelve native binary/toolchain checks to pass. Tag both modules at that commit without moving existing tags, upload those exact archives and checksums to a draft prerelease, and verify installation by tag before publication. Verify the public downloads after publishing.
