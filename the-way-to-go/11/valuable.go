package main

import "fmt"

type stockPosition struct {
	ticker     string
	sharePrice float64
	count      float64
}

/* method to determine the value of a stock position */
func (s stockPosition) getValue() float64 {
	return s.sharePrice * s.count
}

type car struct {
	make  string
	model string
	price float64
}

/* method to determine the value of a car */
func (c car) getValue() float64 {
	return c.price
}

type valuable interface {
	getValue() float64
}

func showValue(asset valuable) {
	fmt.Printf("Value of the assert is %f.\n", asset.getValue())
}

func main() {
	var o valuable = stockPosition{"GOOG", 577.20, 4}
	showValue(o)
	o = car{"BMW", "M3", 66500}
	showValue(o)
}
