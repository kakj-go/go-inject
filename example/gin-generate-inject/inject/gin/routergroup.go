//inject:github.com/gin-gonic/gin/routergroup.go
package gin

import (
	"fmt"
	"github.com/gin-gonic/gin"
)

type RouterGroup struct {
	Handlers HandlersChain
}

type HandlersChain []HandlerFunc

type HandlerFunc struct{}

func (group *RouterGroup) POST(relativePath string, handlers ...gin.HandlerFunc) gin.IRoutes {
	fmt.Println("before post")
	defer fmt.Println("after post")
	return nil
}
