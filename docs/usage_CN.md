# 使用说明

[English](usage.md) · [README](../README_CN.md)

## 原生构建与测试

```sh
go build -a -toolexec="go-inject" .
go build -toolexec="go-inject" -o server ./cmd/server
go test -toolexec="go-inject" -count=1 ./cmd/server
```

注入器作为编译工具代理工作，保留正常 Go 参数、模块/工作区配置和包路径。代理识别父 Go 进程，建立会话并协调新增 import 与 archive 依赖，执行实际工具时保留退出状态。编译器内建和只有汇编实现的函数没有可拦截的普通 Go 函数体。

`-a` 强制重新编译；正常模式由有效规则和源码指纹参与 Go 缓存。规则提供方是普通模块依赖，继续使用 `go mod tidy`、`go.sum`、`replace`、`go.work` 管理。

每个入口通过登记 imports 选择规则。同一次 Go 调用中，共享依赖如果需要生成不同代码，会报告相关入口并失败，应分别执行 Go 命令；结果一致可以共享编译。库的测试应在库包内登记规则，不自动继承 main 包的规则。

## 登记与聚合

```go
//go:build goinject || generate

package main

//go:generate go-inject vendor .

import (
    _ "example.com/app/inject/local"
    _ "example.com/team/rules/all"
)
```

Go 扫描生成指令时启用 `generate`，普通业务编译不启用这两个标签。规则发现只读取 imports，不执行模板初始化。聚合包使用同样的带标签文件 import 子规则包，普通 helper imports 不启用额外规则；同一规则重复发现只执行一次。

## 保存并查看源码

```sh
go build -work -toolexec="go-inject" .
go-inject inspect --json
```

可选的 inspect 命令读取当前工作目录最近一次保留的注入会话，报告包含 `Session` 目录、有效规则和实际源码快照，缓存命中也保留对应产物。查看历史会话时可以显式传入目录；多入口调用需要选择具体会话。

Go 还会打印自己的 `WORK` 目录。编译器版本探测会捕获 stderr，编译诊断也可能被 Go 缓存重放，因此注入器不把临时会话路径当作编译诊断输出；请通过 inspector 定位当前会话。

报告问题时附上 `go-inject version`、`go version`、原生 Go 命令及相关检查报告。

## 生成 vendor

对于支持 vendor 的规则集合，登记文件中的指令由下列命令触发：

```sh
go generate .
go build -mod=vendor .
go test -mod=vendor .
```

Go 不自动运行 generate。规则、依赖、目标平台或相关构建设置变化后需要重新生成。生成器在其 Go 文件所属包的源码目录执行；共享 vendor 应有一份明确计划。多个入口要求相同共享源码时，可以在一条生成指令中列出它们。

只修改 vendor 覆盖的依赖源码。应用/工作区主模块、标准库、runtime，以及路由到 main/testmain 的初始化均拒绝；可以在 vendored 依赖内部新增 init。本地 replace 依赖被 Go 收录进 vendor 时同样可用。参阅 [Gin 示例](../examples/gin/README_CN.md)。

生成时保留已有 vendor 基线，在交付前进行类型校验，通过哈希和进程锁拒绝规划期间变化的输入及手改的受管输出。首次目录创建也有独立可恢复事务。完成后的状态和源码位置使用可迁移路径；提交 `vendor` 与 `.goinject/vendor-state/manifest.json`，正在执行的事务日志和锁属于本地操作文件。

```sh
go-inject vendor --restore
```

恢复是显式维护操作，先校验归属哈希再恢复基线。原生集成使用状态格式 2；beta.1 的旧状态应先用原版本工具恢复，再用新版本生成。遇到未知格式会失败，不猜测或覆盖基线。

## 诊断

| 结果 | 含义与处理 |
|---|---|
| 不适用 | 入口不依赖目标，检查登记 imports 与业务依赖 |
| 缺少文件/函数/类型 | 按实际依赖源码更新规则 |
| 签名或字段失配 | 真实 Go 类型与描述不一致 |
| 无版本变体或变体重叠 | 保证恰好一个实现满足条件 |
| 声明冲突 | 显式新增内容与已有声明重名 |
| import 循环 | 将共享运行时移出被拦截依赖，或使用显式校验的函数桥接 |
| 共享输出不同 | 分别构建相关入口 |
| vendor/状态被编辑 | 先解决编辑冲突，不会静默覆盖 |
| 无法识别父 Go 进程 | 无法安全确定本次构建上下文 |

追踪运行时、contrib 与 vendor 的能力边界见[集成说明](integrations_CN.md)。
