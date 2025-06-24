package main

import "fmt"

var originalString string
var reversedString string

func main() {
	fmt.Printf("请输入一串字符，以回车结束：")

	_, err := fmt.Scanf("%s", &originalString)
	if err != nil {
		panic(err)
	}

	tmp := []rune(originalString)
	for i, j := 0, len(tmp)-1; i < j; i, j = i+1, j-1 {
		tmp[i], tmp[j] = tmp[j], tmp[i]
	}

	reversedString = string(tmp)

	fmt.Printf("%s\n", reversedString)
}
