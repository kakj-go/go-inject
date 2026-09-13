//inject:github.com/gin-gonic/gin/gin.go
//inject:id gin-request
//inject:version >=v1.11.0 <v1.12.0
package v111

import (
	"strconv"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

type Engine struct {
	maxParams uint16
	//inject:add
	goInjectRequests atomic.Uint64
}

//inject:add
func goInjectHeaders(c *gin.Context, requests uint64, params uint16) {
	c.Header("X-Go-Inject-Calls", strconv.FormatUint(requests, 10))
	c.Header("X-Go-Inject-Private", strconv.FormatUint(uint64(params), 10))
	c.Header("X-Go-Inject-Variant", "1.11")
}

//inject:order 10
func (engine *Engine) handleHTTPRequest(c *gin.Context) {
	goInjectHeaders(c, engine.goInjectRequests.Add(1), engine.maxParams)
	c.Header("X-Go-Inject-Order", "10")
}
