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
