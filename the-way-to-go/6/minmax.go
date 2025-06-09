package main

import "fmt"

func main() {
	var minNum, maxNum int
	minNum, maxNum = MinMax(78, 65)
	fmt.Printf("Min: %d, Max: %d\n", minNum, maxNum)
}

func MinMax(a int, b int) (int, int) {
	if a < b {
		return a, b
	}
	return b, a
}
