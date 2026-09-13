# Building tracing integrations

[简体中文](integrations_CN.md) · [Usage](usage.md)

## Responsibilities

The injection engine selects rules, binds actual Go types, changes source, prepares dependencies, and preserves Go control flow. An integration owns span/context data, propagation protocols, sampling, runtime configuration, transport, and backend behavior.

Both native entry points are useful. Compile-time injection is the basis for an agent which also needs application, standard-library, and runtime changes. Vendor generation is useful for a controlled set of third-party libraries whose generated source should be reviewed and versioned.

| Requirement | Compile-time injection | Vendor generation |
|---|---|---|
| Gin/Dubbo/database-driver interception in vendored dependencies | Supported mechanics | Supported mechanics |
| Private methods, required-field projections, explicit fields/helpers | Supported mechanics | Supported inside vendor |
| Request arguments, results, errors and deferred span completion | Supported mechanics | Supported inside vendor |
| net/http, database/sql and other standard-library hooks | Supported Go targets | Outside its scope |
| runtime goroutine state and creation hooks | Supported Go targets | Outside its scope |
| Main/testmain startup injection | Supported mechanics | Outside its scope |
| Initialization inside a vendored package | Supported | Supported |
| SW8 propagation, sampling, reporting, metrics/logging/profiling | Integration implementation | Integration implementation with reduced hook coverage |

The runtime E2E fixtures verify added goroutine fields, snapshot propagation across two generations, independent child context mutation, and typed function bridges to newly added runtime accessors. These prove injection primitives. They do not certify a complete tracer, its memory lifecycle, or its backend protocol.

## What matching SkyWalking Go requires

[SkyWalking Go](https://github.com/apache/skywalking-go) includes tracing, metrics and logging, not only a collection of interception points. Its [runtime instrumentation](https://github.com/apache/skywalking-go/blob/main/tools/go-agent/instrument/runtime/instrument.go) adds state to runtime.g, propagates snapshots in newproc1, and exposes runtime operations. Its [propagation implementation](https://github.com/apache/skywalking-go/blob/main/plugins/core/propagating.go) handles SW8 and correlation data; [reporter components](https://github.com/apache/skywalking-go/tree/main/plugins/core/reporter) own reporting and backend coordination.

The source review on 2026-09-13 found that [go-inject-trace-contrib](https://github.com/kakj-go/go-inject-trace-contrib) contains early Gin and Dubbo templates. Its [go.mod](https://github.com/kakj-go/go-inject-trace-contrib/blob/master/go.mod) still depends on a 2023 SkyWalking plugins/core revision, and its Gin template calls that tracing API. It is not yet an independent replacement agent.

An integration may adapt a compatible existing runtime or implement its own. Removing the SkyWalking Go dependency requires replacing or porting those runtime and reporter responsibilities too, with appropriate license/attribution preservation. Function bridges can expose shared state through typed accessor functions; variable-symbol bridges and generic bridges are not part of this tool's contract.

Runtime hooks need explicit Go-version verification, including stack/scheduler constraints, garbage collection, goroutine reuse, state cleanup, and concurrency. Preserve immutable context snapshots or otherwise provide the integration's synchronization; copying a pointer does not by itself provide isolated mutable state.

## Integration acceptance

1. Define the exact upstream agent version and plugin/version set being replaced.
2. Verify HTTP/RPC/database chains, parent-child spans, propagation headers, panic/error paths and asynchronous work using a recording backend.
3. Verify actual OAP/reporting protocol behavior, reconnects, shutdown flushing, backpressure, sampling, and any claimed metrics/logging/profiling features.
4. Run runtime lifecycle, GC/leak and performance tests on the supported platform/Go combinations.

A vendor-only integration must publish its reduced coverage and use an explicit context-passing strategy where runtime support is unavailable. It cannot claim automatic coverage of arbitrary goroutines and standard-library clients through vendor changes alone. Unsupported targets fail generation rather than being silently omitted.
