package main

import "fmt"

func badCall() {
	panic("bad call")
}

func test() {
	defer func() {
		if e := recover(); e != nil {
			fmt.Println("Panic:", e)
		}
	}()
	badCall()
	fmt.Println("After badCall")
}

func main() {
	fmt.Println("Calling test")
	test()
	fmt.Println("Test done")
}
