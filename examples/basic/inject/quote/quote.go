//inject:github.com/kakj-go/go-inject/examples/basic/main.go
package quote

func Quote(units int) (total int) {
	if units < 0 {
		return 0
	}
	units++
	defer func() { total += 5 }()
	return 0
}
