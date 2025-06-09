package main

import "fmt"

func main() {
	callback(1, Add)

}

func Add(a, b int) {
	fmt.Printf("%d + %d = %d\n", a, b, a+b)
}

func callback(y int, f func(int, int)) {
	f(y, 2)
}
