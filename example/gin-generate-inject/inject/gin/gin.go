//inject:github.com/gin-gonic/gin/gin.go
package gin

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"net"
)

func Default() (__injectResult0 *gin.Engine) {
	fmt.Println("before Default")
	defer func() {
		fmt.Println("after Default")
		fmt.Printf("Handlers len: %v \n", len(__injectResult0.Handlers))
	}()
	return nil
}

type Engine struct {
}

func (engine *Engine) prepareTrustedCIDRs() (__injectResult0 []*net.IPNet, __injectResult1 error) {
	fmt.Println("before prepareTrustedCIDRs")
	defer fmt.Println("after prepareTrustedCIDRs")
	return nil, nil
}
