# go-inject

### Quick start

```shell
go install github.com/kakj-go/go-inject/tools/generate-inject@latest
go install github.com/kakj-go/go-inject/tools/toolexec-inject@latest
```

[generate-inject](example%2Fgin-generate-inject%2FREADME.md)

[toolexec-inject](example%2Fgin-toolexec-inject%2FREADME.md)

### Documentation

#### create inject method

1. create an inject directory to store the injected code. 
2. create the corresponding .go file as needed. 
3. .go file add `//inject:github.com/gin-gonic/gin/routergroup.go` note
4. copy `github.com/gin-gonic/gin/routergroup.go` method code. 
5. If it is a method, since the structure of the object is not in the current package, we can define a structure with the same name, and the parameters and return values are the same

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

> Note that the return on the last line of the code will not be injected into the code

> Due to the code injection mode, writing if,return and other code in the middle of the code will change the logic of the original code. Here, you can dynamically modify the logic of the external library without changing the source code

> For some functions or methods that only return a structure without a name, you can add name like `__injectResult0` to use. For details, please refer to [change result name](example%2Fgin-toolexec-inject%2Finject%2Fgin)

> For structures and parameter return values, external libraries can be used directly, provided that the external library exposes the structure. However, the return value is quite special. If the structure of the return value and the package to be injected are in the same directory, this situation can only be customized. The reason is that the return value name will be dynamically modified above

> Due to the injection mode being a copy function or method body So using global variables inside is not feasible, but using public packages is feasible because imported packages will also be injected into the library

> If you want to access the private parameters of a return value or structure, you can define a custom structure in the file and use your own structure to access it. For details, please refer to [structure](example%2Fgin-toolexec-inject%2Finject%2Fgin%2Fgin.go)


#### create generate.go

Create a new generate.go in the main method file directory, and then import external injection library or self-developed injection lib from import

in package add `//go:generate generate-inject -path ../`

path is the location of the project directory relative to the current directory

```golang
//go:generate generate-inject -path ../
package main

import (
	_ "gin_generate_inject/inject/gin"
	_ "github.com/kakj-go/go-inject-trace-contrib/skywalking/github.com/gin-gonic/gin"
)
```

#### use generate-inject inject code
cd main method file directory

run `go generate`

#### use toolexec-inject inject code

cd main method file directory, change `go build .` to `go build -a -toolexec="toolexec-inject -path ../"`

path is the location of the project directory relative to the current directory

### generate-inject core

The generate-inject core is based on the mechanism of go generate to dynamically modify vendor code to achieve code injection

So it is not possible to inject the Golang src library

The injected code can be seen in the corresponding file of the vendor

### toolexec-inject core

The core of toolexec inject is to dynamically modify temporary files during compilation by adding `-a - toolexec="toolexec inject"` during code construction

The Golang src library can be injected

The modified code cannot be viewed

toolexec: [劫持 Golang 编译](https://www.anquanke.com/post/id/258431)

### thank

[go-build-hijacking](https://github.com/0x2E/go-build-hijacking)

[skywalking-go](https://github.com/apache/skywalking-go)