package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
}

func main() {
	data, err := os.ReadFile("logs.json")
	if err != nil {
		fmt.Println("读取文件失败：", err)
		return
	}

	var logs []LogEntry
	if err := json.Unmarshal(data, &logs); err != nil {
		fmt.Println("JSON文件解析失败：", err)
		return
	}

	// 过滤ERROR日志
	var errorLogs []LogEntry
	for _, log := range logs {
		if log.Severity == "ERROR" {
			errorLogs = append(errorLogs, log)
		}
	}

	// 写入新文件
	output, err := json.MarshalIndent(errorLogs, "", " ")
	if err != nil {
		fmt.Println("序列化 ERROR 日志失败：", err)
		return
	}

	err = os.WriteFile("logs_error.json", output, 0644)
	if err != nil {
		fmt.Println("写入 logs_error.json 失败：", err)
		return
	}

	fmt.Println("写入 logs_error.json 成功。")
}
