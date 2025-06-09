package main

import "fmt"

var num1 int = 10
var num2 int = 20

var sum, product, difference int

func main() {
	sum, product, difference = calc(num1, num2)
	fmt.Println(sum, product, difference)

	sum, product, difference = calc_2(num1, num2)
	fmt.Println(sum, product, difference)
}

func calc(num1, num2 int) (sum, product, difference int) {
	sum = num1 + num2
	product = num1 * num2
	difference = num1 - num2
	return
}

func calc_2(num1, num2 int) (int, int, int) {
	sum = num1 + num2
	product = num1 * num2
	difference = num1 - num2
	return sum, product, difference
}
