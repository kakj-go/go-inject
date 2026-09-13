# 示例

[English](README.md)

每个应用都是独立 Go 模块。安装 [go-inject](../README_CN.md#安装)，进入示例目录执行 `go mod tidy`，再运行对应命令。

| 示例 | 内容 | 后端 |
|---|---|---|
| [basic](basic/README_CN.md) | 本地规则、参数、结果/defer、提前返回、main 初始化 | build/test |
| [http](http/README_CN.md) | 真实 HTTP 请求、请求头、响应修改、body 保持 | build/test |
| [gin](gin/README_CN.md) | 私有方法/字段、新增字段、helper、有序组合 | build/test/vendor |
| [external](external/README_CN.md) | 独立规则模块、聚合、去重、版本变体 | build/test |
| [rules](rules/README_CN.md) | 可复用 HTTP/Gin 规则及聚合包 | 规则库 |

Gin 示例支持 `v1.11.0`、`v1.12.0`，两个源码变体采用互斥的次版本区间。HTTP 服务通过 `httptest` 自动分配端口，执行完成后关闭，不需要外部服务。

使用方通过本地 `replace` 指向 `../rules`，方便直接修改仓库源码。模块路径仍然是 `github.com/kakj-go/go-inject/examples/rules`。使用已发布模块时去掉 replace，依赖 `v0.1.0-beta.2`。

## 自动验证

在仓库根目录执行：

```sh
go build -o go-inject ./cmd/go-inject
python examples/check.py --tool ./go-inject
python examples/check.py gin external --gin-version v1.12.0 --tool ./go-inject
```

Windows 构建并传入 `./go-inject.exe`。也可以通过 `GOINJECT_BINARY` 指定工具。脚本需要 Python 3.10 及以上，只使用标准库，在临时副本中验证二进制和测试，并检查 Gin vendor 生成/恢复及标准库规则的 vendor 拒绝行为。失败后保留目录，`--keep-work` 可在成功后也保留。

测试断言注入后的行为，使用 `go test -toolexec="go-inject" .` 运行。普通 `go test .` 没有这些注入效果；Gin 已生成 vendor 源码后例外。根目录 `go test ./...` 不包含嵌套模块。
