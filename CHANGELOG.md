# Changelog / 更新记录

## beta-0.2 — v0.1.0-beta.2 (2026-09-13)

- Native `go build -toolexec="go-inject"` and `go test`; vendor generation through `go generate`.
- Automatic parent-Go sessions and shared-package conflict detection.
- Real Go type binding, including imported/generic aliases and constant array lengths.
- Real main/test-file targets, portable vendor state, input conflict checks, and transactional first delivery.
- Native E2E coverage, retained-source inspection on cache hits, and tracing integration boundaries.
- Background sessions use their own working directory so completed builds do not lock the application directory on Windows.
- Session shutdown does not recreate removed temporary files; inspection refreshes partial compiler records on demand.
- Breaking change: replace `go-inject build/test` with native `go build/test -toolexec="go-inject"`. Restore beta.1 vendor state with its original tool before regenerating; the new portable state uses format 2.

- 使用原生 Go 构建与测试，通过 go generate 生成 vendor。
- 自动管理父 Go 会话，检测共享包结果冲突。
- 真实 Go 类型绑定、main/测试源码模型与完整首次交付事务。
- vendor 状态可迁移，缓存命中后仍可查看保留源码，明确追踪集成职责与生成模式边界。
- 后台会话使用自身工作目录，避免构建完成后短暂锁住 Windows 应用目录。
- 会话退出后不再重建已删除的临时文件，inspect 按需刷新编译器的部分记录。
- 不兼容变更：使用原生 `go build/test -toolexec="go-inject"` 替代 `go-inject build/test`。beta.1 的 vendor 状态应先用原工具恢复，再重新生成；新的可迁移状态使用格式 2。

## beta-0.1 — v0.1.0-beta.1

The first beta establishes the import-selected Go template contract and a single `go-inject` command.

- `build`, `test`, `vendor`, `vendor --restore`, `inspect`, and `version`.
- Entry-specific local/external rules, aggregation, stable ordering, and version variants.
- Target projections and explicit declaration/field additions, with native Go return/defer semantics.
- Generated-source inspection and vendor ownership/restoration.
- Executable basic, HTTP, Gin, and external-module examples with automated validation.
- Go 1.26 minimum; English and Chinese documentation.

首个 beta 确立通过 import 选择 Go 模板的契约，统一使用 `go-inject` 命令。

- 提供 `build`、`test`、`vendor`、`vendor --restore`、`inspect` 和 `version`。
- 按入口选择本地或外部规则，支持聚合、稳定排序和版本变体。
- 目标投影与显式新增声明/字段，保持原生 Go return/defer 语义。
- 查看生成源码，记录 vendor 文件归属并恢复。
- basic、HTTP、Gin、外部模块可执行示例及自动验证。
- 最低 Go 1.26，提供中英文文档。

This beta is a general injection tool, not a completed tracing agent. Release CI results provide the actual validation evidence. / 本 beta 是通用注入工具，不代表完整追踪 Agent；实际验收证据以发布 CI 结果为准。
