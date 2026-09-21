# 可复用规则

[English](README.md)

模块路径：`github.com/kakj-go/go-inject/examples/rules`

```sh
go get github.com/kakj-go/go-inject/examples/rules@v0.1.0-beta.3
```

在应用入口的 `//go:build goinject` 登记文件中启用规则包：

| import 后缀 | 选择内容 |
|---|---|
| `/http` | `net/http.(*Client).Do` 请求与响应规则 |
| `/gin` | 与目标版本对应的 Gin 请求变体 |
| `/all` | 同时选择 HTTP 和 Gin |

HTTP 修改请求头及返回状态，便于直接观察效果，不是生产追踪集成。Gin 展示私有方法和字段、新增原子字段、helper 注入及 order `10`。

## 变体

| 包 | ID | 目标版本 |
|---|---|---|
| `/gin/v111` | `gin-request` | `>=v1.11.0 <v1.12.0` |
| `/gin/v112` | `gin-request` | `>=v1.12.0 <v1.13.0` |

两个候选具有相同的提供方模块和目标包。聚合包同时导入它们，目标适用时恰好选择一个。实际 Gin 版本由 Go 模块最小版本选择决定；即使规则模块的最低依赖是 1.11，应用仍可使用 1.12。测试基线是 `v1.11.0` 和 `v1.12.0`。

聚合包包含普通 `doc.go`，在未启用登记标签时仍是合法包。带标签 imports 只是发现元数据，不承担运行时初始化。

## 开发和发布

仓库使用方通过 `replace ... => ../rules` 指向本地模块。修改模板后重新构建使用方，即可验证变化。[示例脚本](../README_CN.md#自动验证)检查实际生成行为。

嵌套模块发布标签是 `examples/rules/v0.1.0-beta.3`，`go.mod` 中使用的依赖版本是 `v0.1.0-beta.3`。引擎与可复用规则模块具有独立模块身份及发布标签。
