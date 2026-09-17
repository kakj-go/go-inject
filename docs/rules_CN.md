# 规则参考

[English](rules.md) · [使用指南](usage_CN.md)

## 目标和身份

模板文件在 `package` 前指定目标包和源文件：

```go
//inject:github.com/gin-gonic/gin/gin.go
//inject:id gin-request
//inject:version >=v1.11.0 <v1.12.0
package hook
```

也可以只指定目标包，此时声明在包内任意文件中匹配，而不限于某个源文件：

```go
//inject:github.com/gin-gonic/gin
package hook
```

包级目标必须包含斜杠或首段含点（如 `example.com/lib`、`net/http`）；不含这两者的单词仍然非法，避免拼写错误被静默吞掉。匹配仍是精确的：函数或方法名必须在包内恰好对应一个声明。

使用包含模块主版本后缀的 import 路径，不使用磁盘路径。`//inject:id`、`//inject:version` 可选，版本约束必须配合显式 ID。ID 使用小写字母、数字、`.`、`_`、`/` 和 `-`。未知指令报错，Go 构建约束按实际 Go 版本、平台和标签选择文件。

版本约束由空格分隔的比较表达式组成，多个条件取 AND，例如 `>=v1.11.0 <v1.12.0`。比较运算符为 `=`、`<`、`<=`、`>`、`>=`，版本采用带 `v` 前缀的 Go 语义版本。使用显式比较，不使用 npm 风格的 `^`、`~` 或 `||`。

有版本约束的目标必须具有可验证的模块版本。目标通过本地 replace 替换且版本未知时，不会猜测它符合某个区间。这与本地替换规则提供方不同：规则提供方可以本地开发，变体仍按目标的实际版本选择。

不同版本实现共享显式 ID。变体组由 **提供方模块路径 + ID + 目标包** 组成，可以跨越同一规则模块的不同子包。目标出现在业务依赖图中时必须恰好选中一个实现；零个或多个都报错。没有显式 ID 时，不会把独立规则自动当成互斥变体。

可复用示例通过[带标签的聚合文件](../examples/rules/gin/inject.go)选择 [Gin 1.11](../examples/rules/gin/v111/gin.go) 或 [Gin 1.12](../examples/rules/gin/v112/gin.go)。将变体放在不同包中，也避免编辑器正常分析时出现重复 Go 声明。

## 函数

普通函数和方法声明是拦截模板。描述目标的接收者、参数和结果，名字绑定到对应目标值。类型结构和包身份参与验证，参数的拼写不作为目标身份。

```go
func Quote(units int) (total int) {
    if units < 0 {
        return 0
    }
    units++
    defer func() { total += 5 }()
    return 0
}
```

存在末尾**顶层 return 语句**时将其移除，它的表达式不会求值。模板剩余语句之后接上原函数体。条件分支和闭包中的 return 保留原本含义。无返回值且没有末尾 return 的模板会保留最后一条普通语句。

需要在 defer 闭包中读取或修改返回值时，使用命名结果。`defer f(value)` 在注册时求值参数，`defer func() { f(value) }()` 在执行时读取捕获值。panic、recover、提前返回和 defer 顺序遵守 Go 语义，原函数体不会被移动到额外包装闭包中。

模板局部变量与原函数、其他规则的局部变量分别绑定。改名需保持闭包捕获、标签、接收者、参数和返回值引用的含义。

## 投影和新增声明

同名类型描述真实目标类型。投影可以只列出模板需要的成员，包括私有字段：

```go
type Engine struct {
    maxParams uint16
    //inject:add
    requestCount uint64
}
```

`maxParams` 必须存在且类型兼容，`requestCount` 必须是新字段。生成代码操作目标的真实 `Engine`，不操作占位对象副本。投影声明缺失时报错，不会自动新增。

在声明或字段前紧邻放置 `//inject:add` 表示新增：

```go
//inject:add
const headerName = "X-Example"

//inject:add
func setHeader(c *gin.Context) {
    c.Header(headerName, "enabled")
}
```

helper、类型、变量、常量和字段必须归属明确，不能与既有声明冲突。同一规则包的新增声明和模板之间可以跨文件引用，引用会一起绑定。目标并发执行时必须使用并发安全状态；Gin 示例增加的是 `atomic.Uint64` 字段。

目标局部声明存在于目标包内。进程级共享状态放在真实 helper 或 runtime 包中。将声明复制到多个目标会产生多个声明，不会自动成为共享单例。运行时 helper 的 imports 不能形成返回目标包的依赖环。

同名 `var` 或 `const` 声明未标注 `//inject:add` 时，会替换既有声明的初始化值。需要提供初始化表达式，保持声明种类及分组名字一致，并使用兼容类型。目标缺失或多个规则竞争替换时报错。这会改变目标的初始化行为，不是新增模板局部变量。`//inject:add` 始终表示新增声明，不能用于覆盖既有声明。

## main 初始化

使用 main 目标文件为入口增加初始化：

```go
//inject:main
package startup

import "fmt"

//inject:add
func init() {
    fmt.Println("injection ready")
}
```

在 `//inject:main` 文件中，新增 `init` 路由到应用 main 包，作为生成程序的 Go 初始化函数执行；规则发现本身不会执行它。共享运行时的初始化可封装为普通 helper，由新增的 init 调用。参见 [basic 示例](../examples/basic/inject/quote/startup.go)。

也可以把与版本关联的初始化保留在库目标变体中，只将新增的初始化函数标记为 main：

```go
//inject:github.com/gin-gonic/gin/gin.go
//inject:id gin-bootstrap
//inject:version >=v1.11.0 <v1.12.0
package hooks

import agent "example.com/team/runtime"

//inject:add
//inject:main
func init() {
    agent.Start()
}
```

声明级 `//inject:main` 只适用于带函数体的新增 `func init()`。包含它的库变体选中后，该初始化才路由到 main；排除或不适用的变体不会贡献初始化。库目标文件中只有 `//inject:add` 的普通 init 仍留在目标库。文件级 main 目标则直接选择 main，不会自动受到另一个库规则的条件控制。

移动到 main 的初始化使用 main 作用域，不能直接访问目标包私有名字或目标局部 helper。应调用已导入的 runtime helper，或使用合法的公开包限定引用。vendor 模式拒绝入口初始化。

## 组合

```go
//inject:order 10
func (engine *Engine) handleHTTPRequest(c *gin.Context) {
    c.Header("X-Example", "first")
}
```

order 是有符号整数，默认 `0`。数值小的先进入，同值按稳定规则身份排序，不依赖 import 顺序、源码遍历、临时路径或 map 迭代。

A、B 依次进入时，执行关系为：

```text
A 入口 → B 入口 → 原函数体
原函数 defer → B defer → A defer
```

只执行已注册的 defer。A 提前返回时，B 和原函数都不执行。后执行的 defer 可以观察和修改此前 defer 留下的结果。通过聚合包再次导入 A 不会重复应用。

## imports 和底层 hook

模板 imports 按生成代码的实际需要成为依赖。对目标包自身公开类型的引用需在目标内绑定，不能形成 self import。包别名和名字冲突按源码符号绑定处理。

原生编译指令和链接符号仍需要合法的 Go 签名及链接关系。beta 的链接桥接支持非泛型包级函数。`go:linkname` 不是任意逃生通道，目标符号、签名、依赖闭包和初始化必须适用于所选工具链。底层 runtime hook 需要专门的工具链测试。vendor 模式不支持标准库和 runtime 目标。

生成源码和诊断是规则开发流程的一部分。请在真实目标包中测试行为；投影包自身编译成功不能验证它对目标的假设。
