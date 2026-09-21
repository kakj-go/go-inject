# Changelog / 更新记录

## beta-0.3 — v0.1.0-beta.3 (2026-09-21)

- Package-level injection targets: a template header naming only the target package matches declarations in any file of that package, enabling otelc-style trace rules over packages such as `net/http`. Bare single-word targets stay invalid so typos remain loud.
- Self `//go:linkname X X` symbol renames are valid on value declarations; cross-package linkname bridges remain function-only.
- Hygienic renaming keeps struct-literal field keys spelled as written while template parameters and locals rename; map-literal keys rename normally.
- Legacy modules published without `go.mod` (`github.com/pkg/errors` and friends) resolve through the module proxy instead of a broken directory replacement.
- Supported toolchain series widened to Go 1.24 through 1.27. The module now requires Go 1.25, and CI, archive validation, and install checks cover Go 1.25.8, 1.26.8, and 1.27.1.

- 包级注入目标：模板头只写目标包时，声明在包内任意文件中匹配，支撑 otelc trace 对 `net/http` 等整包注入；单词目标仍然非法，避免拼写错误被静默吞掉。
- 自引用 `//go:linkname X X` 的符号重命名可用于值声明；跨包 linkname 桥接仍仅限函数。
- 模板参数与局部变量改名时保持结构体字面量字段名的原始拼写；map 字面量键正常改名。
- 无 `go.mod` 的历史模块（如 `github.com/pkg/errors`）改由模块代理解析，不再被目录替换破坏。
- 支持的工具链系列扩展到 Go 1.24–1.27。模块要求 Go 1.25；CI、归档验证与安装检查覆盖 Go 1.25.8、1.26.8、1.27.1。

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
