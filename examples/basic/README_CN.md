# 基础示例

[English](README.md) · [全部示例](../README_CN.md)

本模块通过[本地规则](inject/quote/quote.go)修改应用的 `Quote` 函数：参数加一，defer 为最终结果加 `5`，负数参数提前返回。[main 初始化](inject/quote/startup.go)输出启动标记。

```sh
go mod tidy
go-inject test .
go-inject build -work -o basic .
./basic
```

Windows 使用 `-o basic.exe` 和 `./basic.exe`。

预期输出：

```text
injection ready
quote=35 early=0
```

未注入时，`Quote(2)` 返回 `20`，`Quote(-1)` 返回 `-10`。规则末尾的顶层 `return 0` 被移除，条件分支 return 保留。测试还验证了参数为零的情况。[inject.go](inject.go)展示了入口登记。

将 `-work` 输出的会话目录传给 `go-inject inspect --json <目录>`，可以查看生成源码。本例在构建会话中修改应用源码，第三方源码交付参见 [Gin vendor 示例](../gin/README_CN.md)。
