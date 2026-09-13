# Architecture

[简体中文](architecture_CN.md) · [Rule reference](rules.md)

go-inject is a source transformation and build tool. Its public contract is Go imports for selection, Go templates for behavior, and inspectable generated source. It does not implement an observability agent, tracing context, sampling, or exporters.

## Pipeline and boundaries

The public entry points are native Go commands: `go build -toolexec="go-inject"` (and `go test`) and `go generate` for vendor generation. The compiler proxy discovers its parent Go invocation and starts a local build session itself. Registration files use `//go:build goinject || generate`; no registration tag is enabled in business compilation.

This replaces the beta.1 top-level build driver. One native Go invocation shares dependency compilation actions: entries requiring different generated source for a shared package are rejected and must be built separately. Compatible selections share their compilation. Vendor state must be portable between checkouts, and its full validation and first directory delivery belong to the same transaction boundary.

```text
Go command flags and entries
            ↓
Project and business dependency loading
            ↓
Static registration imports → rule packages and variants
            ↓
Target binding, validation, ordering, dependency planning
            ↓
Shared source rewriter
            ↓
Build session / vendor transaction
            ↓
Actual matches, generated source, diagnostics
```

| Responsibility | Owns | Does not own |
|---|---|---|
| CLI | Commands, flags, exit status, user diagnostics | Template interpretation |
| Project loader | Go environment, packages, modules, build constraints | Target-library behavior |
| Rule loader | Static imports, aggregation, identity, variants | Executing rule `init` functions |
| Plan and validation | Applicable targets, signatures, projections, conflicts, dependencies | Filesystem commits |
| Rewriter | Symbol binding, source changes, imports, positions | Running the Go command |
| Build backend | Isolated generated inputs and compiler/linker dependency closure | Vendor ownership |
| Vendor backend | Original state, locking, conflict checks, restore | Standard-library modification |
| Report | Evidence from the actual operation | An independent preview implementation |

These are small pipeline stages and backend adapters. They do not require a service container, dynamic reflection registry, or a factory for every type. Standard Go AST/type tools, decorated syntax trees, and Go's package/module tools supply the parsing and loading mechanisms.

## Two graphs

The **business graph** comes from the requested application entry, without enabling registration files. It determines whether a target is applicable. A template importing Gin for its signature must not make Gin a business dependency by itself.

The **generated build graph** includes imports actually needed by generated source and initialization. It must have complete compiler and linker inputs, valid initialization, and no cycles. Source insertion after Go has scheduled its build cannot be treated as merely adding a line to `importcfg`.

Selections belong to entries. Separate Go invocations isolate different entry selections. Within one native invocation, differing shared-package results are rejected. A physical vendor tree has one result, so incompatible entry results are rejected.

## Source semantics

Templates bind to real target declarations. Receiver, parameter, and result names are mapped by their roles. Type matching retains package identity and complete type structure. A projection validates the target's required members; explicit additions create target-local declarations or fields.

The rewriter preserves control flow, compiler directives, and diagnostic source information. It does not wrap the original body in an extra closure. Template locals and new declarations need stable symbol ownership. The trailing top-level return is removed structurally; returns in branches and closures keep their meaning.

Order is part of the plan, not an artifact of repeated text prepending. Equal-order rules use stable identity. Repeated imports deduplicate, while incompatible additions fail.

## State and dependencies

Target-local helpers can use private target declarations. A shared runtime package should expose a deliberately small dependency surface and avoid importing its instrumented consumers. Copying package-local state into several targets does not produce shared state.

Low-level link bridges require verified signatures, link closure, and initialization. A compiler hook does not waive Go's type rules. Runtime structure changes and standard-library hooks need toolchain-specific verification, independent of ordinary library examples.

## Reproducibility and file ownership

A build session records its selected rules, module/toolchain inputs, target sources, generated files, and result. Cache identity must change when behavior-affecting inputs change; warm-cache tests must change only a rule and verify the new binary behavior. Parallel sessions must not share mutable build-path records.

Vendor generation is a transaction over explicitly owned files. It preserves original contents, checks for user edits and stale output, and restores recorded state. Locks and recovery data handle interrupted operations. A successful second invocation must not stack a second injection onto its own output.

## Implementation precedents

- [go-build-hijacking](https://github.com/0x2E/go-build-hijacking) demonstrates compile input substitution and import configuration.
- [SkyWalking Go](https://github.com/apache/skywalking-go) demonstrates method and structure enhancement, runtime bridges, and entry initialization. Its agent runtime is separate from this tool's scope.
- [Orchestrion](https://github.com/DataDog/orchestrion) demonstrates import-selected rules, generated dependencies, and build integration.
- [Garble](https://github.com/burrowers/garble) demonstrates a Go build driver with transformation-aware cache identity and compiler delegation.

These precedents inform boundaries, not a requirement to expose their plugin APIs. Validate this implementation using [the test contract](testing.md).
