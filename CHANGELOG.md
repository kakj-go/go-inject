# Changelog / 更新记录

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
