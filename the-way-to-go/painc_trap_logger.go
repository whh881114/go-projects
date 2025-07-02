package main

import (
	"fmt"
	"os"
)

func main() {
	logFile, err := os.Create("error.log")
	if err != nil {
		panic(err)
	}
	defer func() {
		fmt.Fprintln(logFile, "程序出错了，请检查输入。") // 应该写入error.log
	}()

	// 读取一个不存的文件
	_, err = os.ReadFile("fake_file.json")
	if err != nil {
		panic(err) // 注意，这里会跳过defer的细节
	}

	fmt.Println("程序执行完毕")
}
