package main

import "fmt"

func main() {
	var score int
	fmt.Printf("请输入你的分数：")
	_, err := fmt.Scan(&score)
	if err != nil {
		panic(err)
		return
	}

	if score < 0 || score > 100 {
		panic("请输入0-100之前的数字。")
		return
	}

	switch {
	case score >= 90:
		fmt.Println("A")
	case score >= 80:
		fmt.Println("B")
	case score >= 70:
		fmt.Println("C")
	case score >= 60:
		fmt.Println("D")
	default:
		fmt.Println("E")
	}
}
