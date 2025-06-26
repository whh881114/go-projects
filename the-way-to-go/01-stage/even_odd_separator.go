package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// 从用户输入的一串整数中，将其分为奇数和偶数，分别存入两个独立的列表中，并分别打印结果。

func main() {
	var evenSlice []int
	var oddSlice []int

	evenSlice = make([]int, 0)
	oddSlice = make([]int, 0)

	fmt.Printf("请输入一段数字，以回车结束：")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	data := strings.TrimSpace(input)

	if !validate(data) {
		panic("输入内容不合法，必须要求全是数字。")
	}

}

func validate(data string) bool {
	pattern := "^\\d+$"
	re := regexp.MustCompile(pattern)
	if re.MatchString(data) {
		return true
	}
	return false
}
