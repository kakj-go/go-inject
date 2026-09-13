# Repository and release scripts

[简体中文](README_CN.md) · [Testing](../docs/testing.md)

These scripts use Python 3.10+ and its standard library. Run them from a checkout.

| Script | Purpose |
|---|---|
| `check_repository.py` | Check repository-local Markdown links/anchors and keep first-party Go files below 2,000 lines |
| `configure_cgo.py` | Resolve and exercise native GCC/Clang, then export `CC`, `CGO_ENABLED=1`, and the macOS SDK when applicable |
| `check_toolchain.py` | Reject a Go version, host/target architecture, CGO setting, or toolchain policy that differs from the native job |
| `package_release.py` | Build six CGO-free binaries with Go 1.27.1 and create deterministic archives plus checksums |
| `verify_release.py` | Verify archive bytes, members, embedded metadata, and actual binary behavior with the active Go toolchain |
| `verify_install.py` | Install a remote CLI/rule module revision with empty caches and verify aggregate rule behavior |

```sh
python scripts/check_repository.py
python scripts/check_snippets.py
python -m unittest discover -s scripts -p 'test_*.py'
```

## CI

[CI](../.github/workflows/ci.yml) runs native tests and executable examples on six platforms with Go `1.26.8` and `1.27.1`, with `GOTOOLCHAIN=local`:

| OS/architecture | Runner | C compiler |
|---|---|---|
| Linux amd64 | `ubuntu-24.04` | Native GCC |
| Linux arm64 | `ubuntu-24.04-arm` | Native GCC |
| Windows amd64 | `windows-2025` | Native GCC from the runner image |
| Windows arm64 | `windows-11-arm` | CGO disabled; no race job |
| macOS amd64 | `macos-15-intel` | `xcrun --sdk macosx --find clang` and its SDK |
| macOS arm64 | `macos-15` | `xcrun --sdk macosx --find clang` and its SDK |

Compiler setup prints the exact compiler version, compiles and runs a C program using standard headers and, on Unix, pthreads. On macOS it exports `SDKROOT` from `xcrun --sdk macosx --show-sdk-path` to both the probe and subsequent Go processes. Missing headers, SDKs, or execution support fail configuration; CGO is not silently disabled.

Unix compiler directories are never prepended to PATH, so `/usr/bin/go` cannot override setup-go. Only Windows needs the GCC directory on PATH for runtime DLLs, and the selected Go directory remains ahead of it. A subsequent toolchain guard checks the actual Go executable, exact version, host and target OS/architecture, `GOTOOLCHAIN=local`, and expected CGO setting. The release build and archive-validation jobs apply the same guard.

Separate five-platform jobs run `go test -race ./internal/...` with both Go versions. They do not substitute cross-compilation for native execution. Linux amd64 additionally runs both Gin dependency baselines.

## Build archives locally

Activate Go 1.27.1, then run:

```sh
python scripts/package_release.py --version v0.1.0-beta.1 --output dist
```

The script builds Linux, Windows, and macOS on amd64/arm64 with `CGO_ENABLED=0`, `-trimpath`, and `-buildvcs=false`. Linker flags embed `main.version` and the full `main.commit`. Archives contain the executable, `README.md`, `README_CN.md`, `LICENSE`, and `THIRD_PARTY_LICENSES.txt`. Windows uses ZIP; other targets use tar.gz. `SHA256SUMS` covers the six archives. Existing artifact names are never overwritten.

Archive timestamps use `SOURCE_DATE_EPOCH` or the commit timestamp. Run `python scripts/package_release.py --help` for arguments. A successful cross-build does not establish runtime support.

## Validate the exact artifact

On a matching native host with the stated Go version:

```sh
python scripts/verify_release.py --directory dist --version v0.1.0-beta.1 --commit <full-commit-id> --goos linux --goarch amd64 --go-version 1.26.8
```

The verifier checks `SHA256SUMS`, permits only the expected regular archive members, validates embedded version/commit and CGO-free Go 1.27.1 build metadata, then executes the extracted binary against the basic and HTTP examples. It does not rebuild the tool under test.

[Release artifacts](../.github/workflows/release-artifacts.yml) builds once and downloads those same archives in twelve native validation jobs: six platforms, each with both supported Go versions. The workflow only uploads Actions artifacts with read-only repository permissions. It does not create, publish, or update a GitHub Release.

For `v0.1.0-beta.1`, the public release title is `beta-0.1` and the release is a prerelease. Publish only after the workflow and the corresponding source CI results pass. Release publication and the nested rules-module tag are separate maintainer actions.
