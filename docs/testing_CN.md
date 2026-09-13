# 测试

[English](testing.md) · [贡献指南](../CONTRIBUTING_CN.md)

## 本地验证

```sh
python scripts/check_repository.py
python scripts/check_snippets.py
python -m unittest discover -s scripts -p 'test_*.py'
go test -timeout=30m ./...
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

完整 E2E 会多次调用真实 Go 编译器，包级总超时显式设为 30 分钟，让较慢的原生 runner 也能跑完整套用例。每个子进程仍保留独立的较短超时，用于检测构建卡住。

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

原生 CI 保留测试详细输出，记录冷热构建耗时、编译次数以及空操作模板的分配次数。**Remote installation** 工作流在打标签前按固定提交 SHA、打标签后按发布版本分别安装 CLI 和聚合规则模块。验证使用空模块缓存与构建缓存、公共 Go 代理和校验数据库，应用中不设置本地 `replace`。发布前两个冻结的 Go 版本都必须通过。

## 原生集成回归

E2E 还覆盖跨包/泛型别名、数组常量、泛型接收者、真实 main 辅助函数和内部测试文件。vendor 用例通过 go generate 验证生成、错误代码在交付前被拒绝、迁移 checkout、恢复、输入编辑保护和首次目录交付恢复。多入口结果一致时共享编译，冲突时明确失败；覆盖率参数和新增依赖 archive 使用原生 go test 验证。

上下文用例验证 runtime 存储、快照回调和访问新增 runtime 函数的类型校验桥接，不依赖遥测厂商。这是基础机制的证据，不是完整 Agent 兼容声明。

## beta 范围

这些测试验证通用注入工具，不证明 SkyWalking Agent 兼容、trace 上报、上下文传播、遥测性能或 OAP 集成。具体集成需要自己的运行时和端到端验收。
