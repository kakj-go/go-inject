package main

import "fmt"

func Quote(units int) int {
	return units * 10
}

func main() {
	fmt.Printf("quote=%d early=%d\n", Quote(2), Quote(-1))
}
