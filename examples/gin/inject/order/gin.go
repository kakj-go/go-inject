//inject:github.com/gin-gonic/gin/gin.go
package order

import "github.com/gin-gonic/gin"

type Engine struct{}

//inject:order 20
func (engine *Engine) handleHTTPRequest(c *gin.Context) {
	c.Header("X-Go-Inject-Order", c.Writer.Header().Get("X-Go-Inject-Order")+",20")
}
