# Gin 与 vendor

[English](README.md) · [全部示例](../README_CN.md)

本例通过 Gin 处理两次真实 HTTP 请求，组合可复用规则和[本地 order-20 规则](inject/order/gin.go)。

可复用规则拦截私有 `Engine.handleHTTPRequest`，读取私有 `maxParams`，增加 `atomic.Uint64` 请求计数器，并新增写入响应头的 helper。它的 order 为 `10`，所以响应记录 `10,20`。两次请求验证新增字段在同一 engine 上保留状态。

```sh
go mod tidy
go test -toolexec="go-inject" .
go build -toolexec="go-inject" -work -o gin-example .
./gin-example
```

Windows 使用 `-o gin-example.exe` 和 `./gin-example.exe`。

预期输出：

```text
PASS gin: private method, private field, added field, helper, order, variant
```

## 通过 vendor 使用原生 Go

```sh
go generate .
go test -mod=vendor .
go build -mod=vendor -o gin-example .
./gin-example
go-inject vendor --restore
```

打开 `vendor/github.com/gin-gonic/gin` 可查看实际生成源码。重复执行 `go generate .` 不能叠加注入。`--restore` 恢复记录中的原始 vendor 状态，并拒绝丢弃用户随后做出的修改。

## 版本变体

仓库模块选择 Gin `v1.11.0`。验证另一个支持基线：

```sh
go get github.com/gin-gonic/gin@v1.12.0
go mod tidy
go test -toolexec="go-inject" .
```

更换依赖版本前先恢复已生成的 vendor。聚合包根据目标模块版本，在 [v111](../rules/gin/v111/gin.go) 和 [v112](../rules/gin/v112/gin.go) 中恰好选一个。测试读取 `gin.Version`，检查相应变体写入的响应头。

在临时副本验证两个基线，可使用[示例验证脚本](../README_CN.md#自动验证)。
