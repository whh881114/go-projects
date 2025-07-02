package main

import (
	"fmt"
	"os"
)

func main() {
	// 准备日志文件
	logFile, err := os.Create("error.log")
	if err != nil {
		fmt.Println("创建日志文件失败：", err)
		return
	}
	defer logFile.Close()

	// 捕获 panic 的 defer
	defer func() {
		if r := recover(); r != nil {
			// 在这里完成 panic 拦截 + 日志记录
			fmt.Fprintf(logFile, "捕获 panic：%v\n", r)
			// 给用户一个友好的提示
			fmt.Println("程序遇到问题，但已优雅退出。")
		}
	}()

	// 触发 panic 的地方
	_, err = os.ReadFile("fake_file.json")
	if err != nil {
		panic(err)
	}

	fmt.Println("程序正常结束")
}
