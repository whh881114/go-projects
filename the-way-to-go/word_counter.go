package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// 从控制台读取一行文本，统计其中英文单词的出现频率。
//
// 英文单词定义为：仅包含英文字母的字符串（a-z、A-Z）；
// 所有非字母（数字、中文、符号等）组成的词项将被忽略；
// 英文单词大小写不敏感，应统一转为小写处理；
// 最后按单词的出现顺序打印每个单词及其出现次数。

func main() {
	var sentence string
	var sentenceSlice []string
	var wordOrder []string
	var wordMap map[string]int

	sentenceSlice = make([]string, 0)
	wordOrder = make([]string, 0)
	wordMap = make(map[string]int)

	fmt.Printf("请输入一段英语句子，以回车结束：")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	sentence = strings.TrimSpace(input)

	words := strings.Fields(sentence)
	for i := range words {
		word := strings.TrimRight(words[i], ".,?!:;\"'") // 当前的过滤字符只有部分符号
		if filterWord(word) {
			sentenceSlice = append(sentenceSlice, word)
		}
	}

	for _, word := range sentenceSlice {
		if _, exists := wordMap[word]; !exists {
			wordOrder = append(wordOrder, word)
		}
		wordMap[word]++
	}

	for _, word := range wordOrder {
		fmt.Printf("%s: %d\n", word, wordMap[word])
	}

}

func filterWord(word string) bool {
	word = strings.ToLower(word)
	pattern := "^[a-z]+'*[a-z]*"
	re := regexp.MustCompile(pattern)
	if re.MatchString(word) {
		return true
	}
	return false
}
