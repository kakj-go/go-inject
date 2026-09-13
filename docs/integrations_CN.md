# 构建追踪集成

[English](integrations.md) · [使用说明](usage_CN.md)

## 职责边界

注入工具负责规则选择、真实 Go 类型绑定、源码改写、依赖准备和 Go 控制流语义。集成模块负责 span/context 数据、传播协议、采样、运行时配置、传输与后端行为。

两种原生入口各有用途。需要改写应用、标准库和 runtime 的完整 Agent，应以构建时注入为基础；vendor 生成适合范围明确的第三方库，便于审查源码并纳入版本管理。

| 需求 | 构建时注入 | vendor 生成 |
|---|---|---|
| vendored Gin、Dubbo、数据库驱动的拦截点 | 提供基础机制 | 提供基础机制 |
| 私有方法、必要字段投影、显式新增字段/helper | 提供基础机制 | 限 vendor 内 |
| 请求参数、返回值、错误和 defer 结束 span | 提供基础机制 | 限 vendor 内 |
| net/http、database/sql 等标准库拦截 | 支持相应 Go 目标 | 不在其范围内 |
| runtime goroutine 状态与创建拦截点 | 支持相应 Go 目标 | 不在其范围内 |
| main/testmain 启动注入 | 提供基础机制 | 不在其范围内 |
| vendored 包内部初始化 | 支持 | 支持 |
| SW8、采样、上报、metrics/logging/profiling | 由集成模块实现 | 由集成模块实现，拦截覆盖受限 |

runtime E2E 已验证新增 goroutine 字段、两代 goroutine 的快照传播、子上下文独立修改，以及访问新增 runtime 函数的类型校验桥接。这些验证的是注入基础能力，不等于完整 tracer、内存生命周期和后端协议已通过验收。

## 达到 SkyWalking Go 类能力还需要什么

[SkyWalking Go](https://github.com/apache/skywalking-go) 包含 tracing、metrics、logging 等完整能力。[runtime 改写](https://github.com/apache/skywalking-go/blob/main/tools/go-agent/instrument/runtime/instrument.go)增加 runtime.g 状态，在 newproc1 中传播快照，并提供 runtime 操作；[传播实现](https://github.com/apache/skywalking-go/blob/main/plugins/core/propagating.go)处理 SW8 和关联数据；[reporter](https://github.com/apache/skywalking-go/tree/main/plugins/core/reporter)负责上报和后端协调。

2026-09-13 的源码核对显示，[go-inject-trace-contrib](https://github.com/kakj-go/go-inject-trace-contrib) 目前是早期 Gin、Dubbo 模板。[go.mod](https://github.com/kakj-go/go-inject-trace-contrib/blob/master/go.mod)仍依赖 2023 年的 SkyWalking plugins/core，Gin 模板直接调用该 tracing API，目前还不是独立替代 Agent。

contrib 可以适配兼容的现有运行时，也可以独立实现；如果目标是完全移除 skywalking-go 依赖，还必须迁移或实现对应 core、reporter 等职责，并保留必要许可证和归属说明。共享状态可以通过经过签名校验的函数访问器桥接；变量符号桥接和泛型桥接不属于本工具的契约。

runtime 拦截必须按 Go 版本验证栈、调度、GC、goroutine 复用、状态清理和并发行为。上下文应使用不可变快照或集成自己的同步机制；仅复制一个指针不代表可变状态已经隔离。

## 集成验收

1. 固定要替代的上游 Agent 版本和插件/依赖版本范围。
2. 用记录型后端验证 HTTP/RPC/数据库调用链、父子 span、传播头、panic/error 和异步任务。
3. 验证真实 OAP/上报协议、重连、退出 flush、背压、采样，以及声明支持的 metrics/logging/profiling。
4. 在支持的平台与 Go 组合上验证 runtime 生命周期、GC/泄漏和性能。

仅使用 vendor 的集成必须说明覆盖范围，在缺少 runtime 支持的位置采用明确的 context 传递策略。仅修改 vendor 无法宣称自动覆盖任意 goroutine 和标准库客户端；不支持的目标应直接导致生成失败。
