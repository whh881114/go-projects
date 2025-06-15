package main

import "fmt"

type Shape interface {
	Area() float64
}

type Square struct {
	side float64
}

func (sq *Square) Area() float64 {
	return sq.side * sq.side
}

func main() {
	sq1 := new(Square)
	sq1.side = 5
	areaIntf := sq1
	fmt.Println("Area of square is ", areaIntf.Area())
}
