package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type LogFilterBySeverity struct {
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

	var logs []LogFilterBySeverity
	if err := json.Unmarshal(data, &logs); err != nil {
		fmt.Println("JSON文件解析失败：", err)
		return
	}

	for _, log := range logs {
		if log.Severity == "ERROR" {
			fmt.Printf("[%s] %s %s\n", log.Timestamp, log.Severity, log.Message)
		}
	}
}
