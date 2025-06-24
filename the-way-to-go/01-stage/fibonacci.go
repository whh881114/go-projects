package main

import "fmt"

func main() {
	n := 30
	fibNums := fibList(n)
	for _, val := range fibNums { //  0, 1, 1, 2, 3, 5, 8, 13, 21, 34, ...
		fmt.Printf("%d ", val)
	}
	fmt.Println()
}

func fibList(n int) []int {
	result := make([]int, 0, n)
	for i := 0; i < n; i++ {
		result = append(result, fib(i+1))
	}
	return result
}

func fib(n int) int {
	if n == 1 {
		return 0
	} else if n == 2 {
		return 1
	} else if n == 3 {
		return 1
	} else {
		return fib(n-1) + fib(n-2)
	}
}
