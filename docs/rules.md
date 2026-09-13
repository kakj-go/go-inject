# Rule reference

[简体中文](rules_CN.md) · [Usage](usage.md)

## Targets and identity

A template file names its target package and source file before `package`:

```go
//inject:github.com/gin-gonic/gin/gin.go
//inject:id gin-request
//inject:version >=v1.11.0 <v1.12.0
package hook
```

Use import paths, including module major-version suffixes, rather than filesystem paths. `//inject:id` and `//inject:version` are optional; a version constraint requires an explicit ID. IDs use lowercase letters, digits, `.`, `_`, `/`, and `-`. Unknown instructions are errors. Go build constraints select files for the actual Go version, platform, and tags.

Version constraints are whitespace-separated comparisons joined with AND, for example `>=v1.11.0 <v1.12.0`. The supported comparison operators are `=`, `<`, `<=`, `>`, and `>=`. Use Go semantic versions with a `v` prefix. This is not an npm range language: use explicit comparisons instead of `^`, `~`, or `||`.

A constrained target must have a verified module version. A local replacement with no known version cannot be assumed to match a range. This differs from locally replacing the rule provider: provider replacement is supported, while variant selection still uses the target's actual version.

Alternative implementations share an explicit ID. A variant group is **provider module path + ID + target package**; it can span several packages inside that provider module. Exactly one implementation must apply when the target is in the business dependency graph. Zero and multiple matches are errors. Without an explicit ID, independent rules are not implicitly grouped as alternatives.

The reusable example selects [Gin 1.11](../examples/rules/gin/v111/gin.go) or [Gin 1.12](../examples/rules/gin/v112/gin.go) through a [tagged aggregate](../examples/rules/gin/inject.go). Separating the variants into packages also avoids duplicate Go declarations during normal editor analysis.

## Functions

Ordinary function and method declarations are interception templates. Describe the target receiver, parameters, and results; names bind to the corresponding target values. Type structure and package identity matter. Parameter spelling is not target identity.

```go
func Quote(units int) (total int) {
    if units < 0 {
        return 0
    }
    units++
    defer func() { total += 5 }()
    return 0
}
```

The final **top-level return statement**, if present, is removed. Its expressions are not evaluated. The original body follows the template's remaining statements. A return in a branch or closure keeps its normal meaning. A void template without a final return retains its last ordinary statement.

Use named results when reading or changing return values in a deferred closure. `defer f(value)` evaluates arguments when registered; `defer func() { f(value) }()` reads captured values when executed. Panic, recover, early return, and deferred-call ordering follow Go semantics. The original function body is not moved into an artificial wrapper closure.

Template-local variables are separate from locals in the original function or other rules. Binding and renaming must preserve captures, labels, receiver/parameter references, and result references.

## Projections and added declarations

A same-named type describes a target type. A projection can list only the members the template needs, including private fields:

```go
type Engine struct {
    maxParams uint16
    //inject:add
    requestCount uint64
}
```

`maxParams` must exist with a compatible type. `requestCount` must be new. The generated code operates on the target's real `Engine`, not on a copied stand-in object. Missing projected declarations are errors rather than implicit additions.

Place `//inject:add` immediately before a declaration or field to add it:

```go
//inject:add
const headerName = "X-Example"

//inject:add
func setHeader(c *gin.Context) {
    c.Header(headerName, "enabled")
}
```

Helpers, types, variables, constants, and fields must have unambiguous ownership and no collision with existing declarations. References among added declarations and templates bind together, including across files in a rule package. Use concurrency-safe state where the target can execute concurrently; the Gin example adds an `atomic.Uint64` field.

Target-local additions live in the target package. Process-wide shared state belongs to a real helper/runtime package. Copying a declaration into several targets creates several declarations, not a shared singleton. Runtime helper imports must not introduce dependency cycles back to the target.

A same-named `var` or `const` declaration without `//inject:add` replaces the existing declaration's initializer. Supply an initializer, retain the declaration kind and grouped names, and use compatible types. A missing target or competing replacement is an error. This can change target initialization behavior; it is not a new template-local variable. `//inject:add` always means a new declaration and cannot be used to overwrite an existing one.

## Main initialization

Use a main-target file for entry initialization:

```go
//inject:main
package startup

import "fmt"

//inject:add
func init() {
    fmt.Println("injection ready")
}
```

In a `//inject:main` file, the added `init` is routed to the application's main package. It runs as a Go initializer in the generated application; rule discovery itself does not execute it. Put reusable runtime initialization behind a normal helper function and call it from the added initializer. See [the basic example](../examples/basic/inject/quote/startup.go).

Alternatively, keep version-specific initialization inside a library-target variant and mark only its added initializer for the main package:

```go
//inject:github.com/gin-gonic/gin/gin.go
//inject:id gin-bootstrap
//inject:version >=v1.11.0 <v1.12.0
package hooks

import agent "example.com/team/runtime"

//inject:add
//inject:main
func init() {
    agent.Start()
}
```

Declaration-level `//inject:main` is valid only on an added `func init()` with a body. It moves that initializer to main after the containing library variant is selected. An excluded or non-applicable variant contributes no initializer. A plain `//inject:add func init()` in a library-target file stays in that library. A file-level main target instead selects main directly, so it is not implicitly conditional on a separate library rule.

An initializer moved to main has main's scope: target-package private names and target-local helpers are unavailable there. Call imported runtime helpers, or use valid public package-qualified references. Vendor mode rejects entry initialization.

## Composition

```go
//inject:order 10
func (engine *Engine) handleHTTPRequest(c *gin.Context) {
    c.Header("X-Example", "first")
}
```

Order is a signed integer; the default is `0`. Lower values enter first. Equal values use stable rule identity, not import order, source traversal, temporary paths, or map iteration.

If rules A and B enter in that order, execution is:

```text
A entry → B entry → original body
original defers → B defers → A defers
```

Only registered defers execute. If A returns early, B and the original body do not run. A later defer can observe and change the result left by earlier deferred calls. Reimporting A through an aggregate does not apply A twice.

## Imports and low-level hooks

Template imports become code dependencies only as needed by generated source. References to the target's own exported types must bind within the target rather than create a self-import. Package aliases and name collisions are resolved through source bindings.

Native compiler directives and link symbols require their normal Go signatures and linkage. Beta link bridges support non-generic package-level functions. A `go:linkname` declaration is not an unrestricted escape hatch: its destination, signature, dependency closure, and initialization must be valid for the selected toolchain. Low-level runtime hooks require targeted toolchain tests. Vendor mode does not support standard-library or runtime targets.

The generated source and diagnostics are part of the authoring workflow. Test behavior in the actual target package; successful compilation of a standalone projection package does not validate its target assumptions.
