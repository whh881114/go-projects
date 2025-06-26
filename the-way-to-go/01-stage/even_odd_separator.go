package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
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

	tmp := strings.Split(data, "")
	for _, i := range tmp {
		num, err := strconv.Atoi(i)
		if err != nil {
			panic("字符转换数字失败。")
		}
		if num%2 == 0 {
			evenSlice = append(evenSlice, num)
		} else {
			oddSlice = append(oddSlice, num)
		}
	}
	fmt.Printf("偶数：%v\n", evenSlice)
	fmt.Printf("奇数：%v\n", oddSlice)

}

func validate(data string) bool {
	pattern := "^\\d+$"
	re := regexp.MustCompile(pattern)
	if re.MatchString(data) {
		return true
	}
	return false
}
