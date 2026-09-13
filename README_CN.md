# go-inject

[English](README.md)

用普通 Go 模板拦截 Go 函数，通过原生 Go 工具链在编译时注入，或在 vendor 中生成可以直接查看的依赖源码。

**Go 1.26 / 1.27** · [使用说明](docs/usage_CN.md) · [规则参考](docs/rules_CN.md) · [示例](examples/README_CN.md)

## 安装

使用 Go 1.26 或 1.27 安装 **beta-0.2** 预发布版本：

```sh
go install github.com/kakj-go/go-inject/cmd/go-inject@v0.1.0-beta.2
go-inject version
```

把 `GOBIN`，或 `go env GOPATH` 下的 `bin` 目录加入 `PATH`。无需运行时注册或中心插件服务。

## 编写并选择规则

模板先声明目标文件，再用普通 Go 函数描述拦截逻辑。下面来自[本地示例](examples/basic/inject/quote/quote.go)：

```go
//inject:github.com/kakj-go/go-inject/examples/basic/main.go
package quote

func Quote(units int) (total int) {
    if units < 0 {
        return 0
    }
    units++
    defer func() { total += 5 }()
    return 0
}
```

末尾顶层 return 是占位语句；条件提前返回与 defer 保持 Go 语义。原函数返回 `units * 10` 时，注入后 `Quote(2)` 返回 `35`，`Quote(-1)` 返回 `0`。

在入口包集中 import 本地、外部或聚合规则。支持 vendor 的 Gin 示例使用下面的登记文件：

```go
//go:build goinject || generate

package main

//go:generate go-inject vendor .

import (
    _ "github.com/kakj-go/go-inject/examples/gin/inject/order"
    _ "github.com/kakj-go/go-inject/examples/rules/gin"
)
```

规则版本继续由 `go.mod`、`go.sum`、`go get`、`replace` 和 `go.work` 管理。业务编译不启用登记标签；Go 执行 generate 时自动启用 `generate` 标签。

## 方式一：构建时注入

```sh
go build -a -toolexec="go-inject" .
go test -toolexec="go-inject" .
```

Go 负责构建和包调度，go-inject 接收编译输入、准备依赖并提供生成的 Go 源码。目标可以是本次构建涉及的应用、依赖、标准库和 runtime 中的 Go 实现，原始源码不被修改。

`-a` 表示完整重编译，日常构建可以省略；有效规则或源码变化会通过工具指纹使 Go 缓存失效。

```sh
go build -work -toolexec="go-inject" .
go-inject inspect --json
```

inspect 查看当前目录最近一次保留的注入会话，缓存命中也能看到对应源码；也可以显式提供会话目录。这是可选诊断命令，构建入口仍是 Go。

参阅[本地](examples/basic/README_CN.md)、[HTTP](examples/http/README_CN.md)与 [Gin](examples/gin/README_CN.md) 可运行示例。

## 方式二：生成 vendor 源码

规则目标属于 vendor 依赖时，使用上面的生成指令：

```sh
go generate .
go build -mod=vendor .
go test -mod=vendor .
```

生成器在需要时创建 vendor，校验计划生成的代码，然后通过事务交付结果。已有 vendor 补丁作为基线保留；重复生成从基线开始，手动修改过的受管生成文件会报告冲突。

将 `vendor` 与 `.goinject/vendor-state` 一起提交。完成后的状态记录相对文件标识、基线、输出哈希、规则归属和选择计划，可在其他 checkout 继续生成或恢复。需要撤销工具改写时，执行 `go-inject vendor --restore`。

Go build/test 不会自动执行 generate。规则、依赖版本或相关平台与构建设置发生变化后，应重新生成。完整流程见 [Gin vendor 示例](examples/gin/README_CN.md)。

### generate 无法注入哪些位置

| 目标 | 构建时注入 | vendor 生成 |
|---|---|---|
| 应用主模块、工作区主模块 | 支持 | 明确拒绝 |
| vendor 覆盖的第三方依赖源码 | 支持 | 支持 |
| net/http、database/sql 等标准库 | 支持 | 明确拒绝 |
| runtime 结构与 Go 实现中的拦截点 | 支持 | 明确拒绝 |
| 路由到应用 main/testmain 的初始化 | 支持 | 明确拒绝 |
| 在 vendored 包中显式新增 init | 支持 | 支持 |

本地 replace 的依赖，只要被 Go 收录进 vendor，也可生成。工具不处理构建图外的任意文件、不具备普通 Go 函数体的汇编/编译器内建函数，也不会把 import 循环自动变成隐式桥接。原生桥接使用经过签名校验的非泛型函数。选中不支持的目标会失败，不会静默跳过。

一份 vendor 对应一套计划。原生多入口 build/test 中，如果共享依赖要求生成不同源码，同样明确报错，应分别构建这些入口；结果一致时可共享 Go 的编译动作。

## 为追踪集成提供基础能力

本工具负责注入机制：私有函数与字段、参数和结果修改、新增声明、初始化、runtime 拦截点与函数桥接。tracer 状态、传播协议、采样、exporter、metrics、logging、profiling 属于 [go-inject-trace-contrib](https://github.com/kakj-go/go-inject-trace-contrib) 等集成模块。

完整 SkyWalking 类集成应以构建时注入为基础。vendor 模式可以覆盖相应依赖库，无法等价提供自动的标准库/runtime 覆盖。完整替代 Agent 还需要独立验证协议、生命周期、版本兼容和后端端到端行为，不能仅凭拦截点迁移完成就宣称等价。参阅[集成边界](docs/integrations_CN.md)。

## 开发与许可证

```sh
go test ./...
go build -o bin/ ./cmd/go-inject
python examples/check.py --tool bin/go-inject
```

Windows 使用 `bin/go-inject.exe`。测试使用临时工程和自动分配端口的本地服务器。参阅[测试](docs/testing_CN.md)、[贡献](CONTRIBUTING_CN.md)和[更新记录](CHANGELOG.md)。

项目参考 [Orchestrion](https://github.com/DataDog/orchestrion)、[SkyWalking Go](https://github.com/apache/skywalking-go)、[go-build-hijacking](https://github.com/0x2E/go-build-hijacking) 与 [Garble](https://github.com/burrowers/garble)。使用 [Apache-2.0](LICENSE) 许可证，发行包包含[第三方许可证说明](THIRD_PARTY_LICENSES.txt)。
