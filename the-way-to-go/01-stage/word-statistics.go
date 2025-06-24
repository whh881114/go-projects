package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"
)

func main() {
	var originalString string
	var statisticsMap map[string]int

	fmt.Printf("请输入一串英文字符，以回车结束：")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	originalString = strings.TrimSpace(input)

	// 判断字符串是否存在非英文字符，使用unicode.Is(unicode.Latin, r)判断，更合理的判断英文范围
	statisticsMap = make(map[string]int)
	if !isAllEnglish(originalString) {
		panic("字符串中除了英文字符外，还存在其他字符。")
	} else {
		for _, v := range originalString {
			_, ok := statisticsMap[string(v)]
			if !ok {
				statisticsMap[string(v)] = 1
			} else {
				statisticsMap[string(v)]++
			}
		}
	}

	// 按母顺序顺序打印
	var words []string
	words = make([]string, 0)
	for k, _ := range statisticsMap {
		words = append(words, k)
	}
	sort.Strings(words)

	fmt.Println("按母顺序顺序打印：")
	for _, word := range words {
		fmt.Printf("%s -> %d\n", word, statisticsMap[word])
	}
}

func isAllEnglish(s string) bool {
	for _, v := range s {
		if !unicode.Is(unicode.Latin, v) {
			return false
		}
	}
	return true
}
