# Testing

[简体中文](testing_CN.md) · [Contributing](../CONTRIBUTING.md)

## Local checks

```sh
python scripts/check_repository.py
python -m unittest discover -s scripts -p 'test_*.py'
go test ./...
go build -o go-inject ./cmd/go-inject
python examples/check.py --tool ./go-inject
```

Use `./go-inject.exe` on Windows. The example runner needs Python 3.10 or newer and only its standard library. `GOINJECT_BINARY` can supply the absolute executable path instead of `--tool`.

Select examples or a Gin version:

```sh
python examples/check.py basic http --tool ./go-inject
python examples/check.py gin external --gin-version v1.11.0 --tool ./go-inject
python examples/check.py gin external --gin-version v1.12.0 --tool ./go-inject
```

The runner copies examples to a temporary directory, resolves their modules, invokes the real CLI, executes binaries, and checks behavior. Services bind to port `0` through `httptest` and close before exit. `--keep-work` retains the copies; failures retain them automatically. `--no-vendor` skips vendor validation.

## What examples assert

| Example | Assertions |
|---|---|
| basic | Parameters, named result modification, early return, main initialization |
| http | Outgoing header, returned status/header, intact response body, unchanged caller request |
| gin | Private method/field, new atomic field, helper, order, version selection |
| external | Separate module, aggregate imports, deduplication, selected version variant |

Gin also verifies repeated vendor generation, ordinary `go build/test -mod=vendor`, binary behavior, and exact restoration of existing vendor and module files. HTTP and external reject vendor because their rules target the standard library; rejection must not change project files.

## Release validation contract

Supported release targets are Linux, Windows, and macOS on amd64 and arm64, using Go 1.26 and 1.27. CI fixes the patch versions to `1.26.8` and `1.27.1`; the [script guide](../scripts/README.md) lists native runner labels, C compilers, race coverage, packaging, and artifact validation. Cross-compiling a binary alone does not prove runtime correctness. Check the release's actual CI results for evidence. This document specifies coverage and does not claim that every matrix job has already passed.

Unit and end-to-end checks must cover:

1. Function/method matching, complete signatures, aliases, projections, generics, and missing-target diagnostics.
2. Return, defer/recover, labels, local variable binding, and composition.
3. Fields and declarations, cross-file helpers, initialization, and declaration conflicts.
4. Local/external/aggregate rules, variants, replacements, workspaces, tags, and entry isolation.
5. Standard-library/runtime mechanisms, new dependencies, link closure, initialization, and cycles.
6. Cold/warm cache behavior, template-only changes, local helper changes, and parallel sessions.
7. Vendor idempotence, ownership, locking, interrupted operations, user edits, and restoration.
8. Inspection output corresponding to actual generated code and executable behavior.

Keep targeted behavioral assertions. A source snapshot or successful compile cannot replace an assertion that the injected behavior ran and the original behavior remained correct.

Native CI uses verbose test output to retain cold/warm build times, compilation counts, and no-op allocation measurements. The **Remote installation** workflow separately installs the CLI and aggregate rule module by an immutable commit SHA before tagging, and by the release version afterward. It uses empty module/build caches, the public Go proxy and checksum database, and an application with no local `replace` directives. Both frozen Go versions must pass before publication.

## Scope of the beta

These tests validate the generic injection tool. They do not establish SkyWalking Agent compatibility, trace delivery, context propagation, telemetry performance, or OAP integration. Such integrations need their own runtime and end-to-end acceptance suites.
