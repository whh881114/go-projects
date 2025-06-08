package main

import (
	"fmt"
	"os"
)

func main() {
	var goos string = os.Getenv("OS")
	fmt.Printf("The operating system is: %s\n", goos)
	path := os.Getenv("PATH")
	fmt.Printf("The path is: %s\n", path)
}
