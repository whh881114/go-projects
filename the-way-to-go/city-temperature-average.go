package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type CityTemperature struct {
	City         string    `json:"city"`
	Temperatures []float64 `json:"temperatures"`
}

func main() {
	data, err := os.ReadFile("temperature.json")
	if err != nil {
		fmt.Println("读取文件失败：", err)
		return
	}

	var cities []CityTemperature
	if err := json.Unmarshal(data, &cities); err != nil {
		fmt.Println("JSON文件解析失败：", err)
		return
	}

	for _, city := range cities {
		sum := 0.0
		for _, temp := range city.Temperatures {
			sum += temp
		}
		avg := sum / float64(len(cities))
		fmt.Printf("%s: %.2f\n", city.City, avg)
	}
}
