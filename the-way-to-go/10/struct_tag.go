package main

import (
	"fmt"
	"reflect"
)

type TagType struct {
	field1 bool   "An important answer.\n"
	field2 string "The name of the thing.\n"
	field3 int    "How much there are.\n"
}

func main() {
	tt := TagType{true, "Barak Obama", 1}
	for i := 0; i < 3; i++ {
		refTag(tt, i)
	}
}

func refTag(tt TagType, ix int) {
	ttType := reflect.TypeOf(tt)
	ixField := ttType.Field(ix)
	fmt.Printf("%v", ixField.Tag)
}
