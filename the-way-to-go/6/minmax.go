package main

import "fmt"

func main() {
	var min, max int
	min, max = MinMax(78, 65)
	fmt.Printf("Min: %d, Max: %d\n", min, max)
}

func MinMax(a int, b int) (int, int) {
	if a < b {
		return a, b
	}
	return b, a
}
