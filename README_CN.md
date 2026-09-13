# go-inject

[English](README.md)

用普通 Go 函数编写拦截逻辑，通过 import 启用规则，再构建包含注入代码的程序，或在 `vendor` 中生成可以直接查看的源码。

**beta-0.1** · Go 1.26 与 1.27 · [规则参考](docs/rules_CN.md) · [可运行示例](examples/README_CN.md)

## 安装

```sh
go install github.com/kakj-go/go-inject/cmd/go-inject@v0.1.0-beta.1
go-inject version
```

从仓库构建时，运行 `go build -o go-inject ./cmd/go-inject`；Windows 使用 `go-inject.exe`。

## 一个简单例子

假设 `example.com/app/price/price.go` 中有：

```go
package price

func Quote(units int) int {
    return units * 10
}
```

在应用中创建 `inject/price/price.go`：

```go
//inject:example.com/app/price/price.go
package price

func Quote(units int) (total int) {
    if units < 0 {
        return 0
    }
    units++
    defer func() { total += 5 }()
    return 0
}
```

末尾顶层 `return` 是占位语句，注入时会被移除，随后接上原函数主体。条件分支里的 `return` 仍然表示提前返回；`defer` 可以读取和修改最终结果。注入后 `Quote(2)` 返回 `35`，`Quote(-1)` 返回 `0`。

在应用入口包创建登记文件，例如 `cmd/server/inject.go`：

```go
//go:build goinject

package main

import _ "example.com/app/inject/price"
```

沿用 Go 的参数和目标构建、测试：

```sh
go-inject build -o server ./cmd/server
go-inject test ./cmd/server
```

登记 imports 为当前入口选择规则。工具静态读取这些 imports，最终业务构建不会启用 `goinject` 标签。某个库自身的测试需要注入时，在该库包目录放置自己的登记文件。完整代码见 [basic 示例](examples/basic/README_CN.md)。

## 使用和共享规则

规则可以放在当前项目、公开模块或私有模块中，使用 `go.mod`、`go.sum`、`replace` 和 `go.work` 管理。带标签的登记文件也可以聚合多个规则包，无需运行时注册。

```go
//go:build goinject

package main

import _ "github.com/kakj-go/go-inject/examples/rules/http"
```

[可复用示例模块](examples/rules/README_CN.md) 提供 HTTP、Gin、聚合和版本变体规则。[HTTP 示例](examples/http/README_CN.md) 修改请求头和响应状态，并保持响应 body。[Gin 示例](examples/gin/README_CN.md) 访问私有方法和字段、增加字段和 helper，并组合两个有序拦截器。

## 查看实际注入代码

```sh
go-inject build -work -o server ./cmd/server
go-inject inspect --json <构建会话目录>
```

`-work` 保留构建会话并输出目录。查看会话可了解本次实际选择的规则、目标版本、命中情况，以及构建使用的生成文件。

第三方依赖也可以直接生成到 `vendor`：

```sh
go-inject vendor ./cmd/server
go build -mod=vendor -o server ./cmd/server
go-inject vendor --restore
```

一份 vendor 对应一套生成结果。入口规则冲突和本地修改会得到明确报告，重复生成不能再次注入已生成源码。vendor 支持第三方依赖规则；应用主模块、main 初始化、标准库及 `runtime` 目标使用 `build` 或 `test`。

## 行为约定

- 已选规则的目标不在当前入口业务依赖中时，报告不适用。
- 目标适用，但函数不存在、签名不兼容、投影字段缺失或版本不支持时，操作失败。
- 多规则按 `//inject:order` 从小到大执行，再按稳定身份排序；默认值为 `0`，`defer` 保持 Go 的逆序执行语义。
- 同名类型是投影。增加字段、helper、类型、变量、常量或初始化时使用 `//inject:add`。
- 同一提供方模块、显式规则 ID、目标包组成的版本变体组，必须恰好选中一个适用实现。

[规则参考](docs/rules_CN.md) 定义完整语义，[使用指南](docs/usage_CN.md) 说明命令和排错，[架构文档](docs/architecture_CN.md) 说明包加载、代码改写、依赖和两个后端的职责。

go-inject 是通用注入工具。追踪运行时、上下文传播、采样和 exporter 由基于它构建的规则与运行时库提供。

## 开发

```sh
go test ./...
go build -o go-inject ./cmd/go-inject
python examples/check.py --tool ./go-inject
```

Windows 使用 `./go-inject.exe`。示例验证在临时副本中运行，HTTP 服务自动分配本地端口。参阅 [测试](docs/testing_CN.md)、[贡献指南](CONTRIBUTING_CN.md) 和 [更新记录](CHANGELOG.md)。

## 致谢

本项目参考了 [go-build-hijacking](https://github.com/0x2E/go-build-hijacking)、[Apache SkyWalking Go](https://github.com/apache/skywalking-go)、[Orchestrion](https://github.com/DataDog/orchestrion) 和 [Garble](https://github.com/burrowers/garble) 的机制及工程经验。具体职责边界见架构文档。

使用 [Apache-2.0](LICENSE) 许可证。二进制发行包包含[第三方许可证说明](THIRD_PARTY_LICENSES.txt)。
