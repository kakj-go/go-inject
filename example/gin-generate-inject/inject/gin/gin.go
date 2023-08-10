//inject:github.com/gin-gonic/gin/gin.go
package gin

import "fmt"

type Engine struct {
	RouterGroup
}

type RouterGroup struct {
	Handlers HandlersChain
}

type HandlersChain []HandlerFunc

type HandlerFunc struct{}

func Default() (__injectResult0 *Engine) {
	fmt.Println("before Default")
	defer func() {
		fmt.Println("after Default")
		fmt.Printf("Handlers len: %v \n", len(__injectResult0.Handlers))
	}()
	return nil
}
