# 测试

[English](testing.md) · [贡献指南](../CONTRIBUTING_CN.md)

## 本地验证

```sh
python scripts/check_repository.py
python -m unittest discover -s scripts -p 'test_*.py'
go test ./...
go build -o go-inject ./cmd/go-inject
python examples/check.py --tool ./go-inject
```

Windows 使用 `./go-inject.exe`。示例脚本需要 Python 3.10 及以上，只使用标准库。也可以通过 `GOINJECT_BINARY` 指定工具绝对路径，代替 `--tool`。

选择示例或 Gin 版本：

```sh
python examples/check.py basic http --tool ./go-inject
python examples/check.py gin external --gin-version v1.11.0 --tool ./go-inject
python examples/check.py gin external --gin-version v1.12.0 --tool ./go-inject
```

脚本将示例复制到临时目录，解析模块，执行真实 CLI、运行二进制并检查行为。服务通过 `httptest` 使用端口 `0`，退出前自动关闭。`--keep-work` 保留副本，失败时自动保留。`--no-vendor` 跳过 vendor 验证。

## 示例断言

| 示例 | 验证内容 |
|---|---|
| basic | 参数、命名返回值修改、提前返回、main 初始化 |
| http | 发出请求头、返回状态和头、body 完整、调用方请求不变 |
| gin | 私有方法和字段、新增原子字段、helper、顺序、版本选择 |
| external | 独立模块、聚合 imports、去重、版本变体选择 |

Gin 还验证重复 vendor 生成、普通 `go build/test -mod=vendor`、二进制行为，以及既有 vendor 和模块文件的精确恢复。HTTP 和 external 包含标准库规则，vendor 请求必须被拒绝，且不能修改项目文件。

## 发布验收契约

发布支持 Linux、Windows、macOS 的 amd64 和 arm64，使用 Go 1.26、1.27。CI 固定补丁版本为 `1.26.8`、`1.27.1`；[脚本指南](../scripts/README_CN.md)列出原生 runner、C 编译器、race 范围、打包及产物验证。仅交叉编译不能证明运行行为正确。证据以对应发布的实际 CI 结果为准，本文说明覆盖范围，不表示所有矩阵任务已经通过。

单元测试及端到端验证需覆盖：

1. 函数和方法匹配、完整签名、别名、投影、泛型、目标缺失诊断。
2. return、defer/recover、标签、局部变量绑定和规则组合。
3. 字段和声明、跨文件 helper、初始化、声明冲突。
4. 本地/外部/聚合规则、变体、replace、工作区、tags、入口隔离。
5. 标准库和 runtime 机制、新增依赖、链接闭包、初始化、依赖环。
6. 冷热缓存、仅改模板、本地 helper 变化、并行会话。
7. vendor 幂等、文件归属、锁、中断恢复、用户编辑和还原。
8. 会话信息与实际生成源码及可执行行为一致。

保留针对行为的断言。源码快照或成功编译不能代替“注入逻辑确实执行，原行为仍然正确”的验证。

## beta 范围

这些测试验证通用注入工具，不证明 SkyWalking Agent 兼容、trace 上报、上下文传播、遥测性能或 OAP 集成。具体集成需要自己的运行时和端到端验收。
