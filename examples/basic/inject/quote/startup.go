//inject:main
package quote

import "fmt"

//inject:add
func init() {
	fmt.Println("injection ready")
}
