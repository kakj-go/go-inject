# 外部模块与聚合

[English](README.md) · [全部示例](../README_CN.md)

[inject.go](inject.go)从独立模块 `github.com/kakj-go/go-inject/examples/rules` 同时导入 `rules/all` 和 `rules/http`。`all` 已经包含 HTTP，请求和响应仍需恰好出现一个注入头，用于证明直接和聚合导入不会重复应用规则。

应用使用 Gin `v1.12.0`。Gin 聚合包导入两个提供方模块和显式 ID 相同的候选，通过版本约束选中 1.12 实现。真实本地 HTTP 请求同时验证 Gin 入口规则与标准库客户端规则，并保持 body。

```sh
go mod tidy
go test -toolexec="go-inject" .
go build -toolexec="go-inject" -work -o external-example .
./external-example
```

Windows 使用 `-o external-example.exe` 和 `./external-example.exe`。

预期输出：

```text
PASS external: module rules, aggregate, deduplication, selected Gin variant
```

本地 `replace => ../rules` 用于开发仓库中的源码。使用已发布模块时去掉该 replacement，再执行：

```sh
go get github.com/kakj-go/go-inject/examples/rules@v0.1.0-beta.3
go mod tidy
```

源码 imports 不需要变化。团队自己的规则同样通过公开或私有 Go 模块分发。

当前选择的 `rules/all` 包含标准库 HTTP 规则，因此 vendor 请求会被拒绝。[vendor 示例](../gin/README_CN.md)只选择 `rules/gin`。
