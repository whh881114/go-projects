package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	var originalString string
	var reversedString string

	// 使用fmt.Scanf()时，读取到以空格隔开的字符串时，会缓存到标准输入中。
	// fmt.Printf("请输入一串字符，以回车结束：")
	// _, err := fmt.Scanf("%s", &originalString)
	// if err != nil {
	// 	panic(err)
	// }

	fmt.Printf("请输入一串字符，以回车结束：")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	originalString = strings.TrimSpace(input)

	// 把字符串转成 rune 切片（每个 rune 是一个 Unicode 字符）
	tmp := []rune(originalString)
	for i, j := 0, len(tmp)-1; i < j; i, j = i+1, j-1 {
		tmp[i], tmp[j] = tmp[j], tmp[i]
	}

	// 转回字符串并输出
	reversedString = string(tmp)
	fmt.Printf("对原始字符串进行反向输出：%s\n", reversedString)
}
