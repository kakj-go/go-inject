# go-inject

### Quick start

```shell
go install github.com/kakj-go/go-inject/tools/generate-inject@latest
go install github.com/kakj-go/go-inject/tools/toolexec-inject@latest
```

[generate-inject](example%2Fgin-generate-inject%2FREADME.md)

[toolexec-inject](example%2Fgin-toolexec-inject%2FREADME.md)

### Instructions

#### 创建 inject 方法

首先创建一个 inject 目录用于存放注入的代码，然后根据需要创建对应的.go文件, 文件开头需要加入注释 `//inject:github.com/gin-gonic/gin/routergroup.go`注释的内容就是你要拦截的文件
。代码里面你就可以将对应的文件的函数或者方法粘贴过来并清空方法体。这时候如果是方法由于对象的结构体不在当前包中，所以我们可以定义一个同名的结构体，参数和返回值同理。

```golang
//inject:github.com/gin-gonic/gin/routergroup.go
package gin

import (
	"fmt"
	"github.com/gin-gonic/gin"
)

type IRoutes interface {
}

func (group *RouterGroup) POST(relativePath string, handlers ...gin.HandlerFunc) IRoutes {
	fmt.Println("before POST")
	defer fmt.Println("after POST")

	return nil
}
```

> 注意代码的最后一行 return 是不会注入到三方库中

> 由于是代码注入的模式，如果在代码中间写 if return 之类的代码会改变原代码的逻辑。这里就可以动态修改三方库的逻辑而不用动源码

> 对于有些函数或者方法返回值只有结构体而没有名称的，你可以根据返回值的位置加上对应的 `__injectResult0` 这种名称，具体可以参考 [修改返回值](example%2Fgin-toolexec-inject%2Finject%2Fgin)

> 对于结构体和参数返回值是可以直接用三方库的，前提是三方库吧其暴露出来即可。但是返回值比较特殊，如果返回值的结构体和要注入的包在同一目录这种情况只能自定义了。其原因是上面会动态修改返回值名称

> 由于注入的模式是拷贝函数或者方法的 body. 所以在里面使用全局变量是不太行的，但是使用公共包是可行的，因为 import 的包也会注入到三方库中

> 返回值或者结构体如果想访问其私有参数，可以在文件中定义自定义结构体，使用自己的结构体就可以访问了。具体参考 [定义结构体](example%2Fgin-toolexec-inject%2Finject%2Fgin%2Fgin.go)


#### 创建 generate 声明

在 main 方法文件目录下新建 generate.go. 然后 import 中就可以导入三方的注入代码或者自己开发的注入代码。在 package 加入 `//go:generate generate-inject -path ../`
这行注释。其中 path 是项目目录相对于当前目录的位置

```golang
//go:generate generate-inject -path ../
package main

import (
	_ "gin_generate_inject/inject/gin"
	_ "github.com/kakj-go/go-inject-trace-contrib/skywalking/github.com/gin-gonic/gin"
)
```

#### 使用 generate-inject 注入代码
cd 到对应的 main 目录，执行 go generate。然后就可以愉快的玩耍了

#### 使用 toolexec-inject 注入代码
cd 到对应的 main 目录, 修改 `go build .` 为 `go build -a -toolexec="toolexec-inject -path ../"`。其中 path 是项目目录相对于当前目录的位置

### generate-inject 说明

generate-inject 核心是基于 go generate 的机制动态修改 vendor 代码来实现代码注入的。所以对于 golang src 库是无法注入的。其注入的代码可以在 vendor 对应的文件中看到

### toolexec-inject 说明

toolexec-inject 核心是在代码构建的时候加上 -a -toolexec="" 来动态修改编译时候的临时文件来实现的，golang src 库是可以注入的

toolexec 原理说明参考文章: [劫持 Golang 编译](https://www.anquanke.com/post/id/258431)

### 致谢以下仓库提供的灵感

[go-build-hijacking](https://github.com/0x2E/go-build-hijacking/tree/main)

[skywalking-go](https://github.com/apache/skywalking-go)