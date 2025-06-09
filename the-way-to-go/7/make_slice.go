package main

import "fmt"

func main() {
	var slice1 []int = make([]int, 10)

	for i := 0; i < len(slice1); i++ {
		slice1[i] = i * 5
	}

	for i := 0; i < len(slice1); i++ {
		fmt.Printf("slice1[%d]=%d\n", i, slice1[i])
	}

	fmt.Printf("len=%d\n", len(slice1))
	fmt.Printf("cap=%d\n", cap(slice1))
}
