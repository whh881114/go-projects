package main

import "fmt"

func trace(s string)   { fmt.Printf("Entering: %s\n", s) }
func untrace(s string) { fmt.Printf("Leaving: %s\n", s) }

func a() {
	trace("a")
	defer untrace("a")
	fmt.Println("Inside: a")
}

func b() {
	trace("b")
	defer untrace("b")
	fmt.Println("Inside: b")
	a()
}

func main() {
	b()
}
