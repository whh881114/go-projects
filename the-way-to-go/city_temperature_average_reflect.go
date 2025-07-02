package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
)

type CityTemperatureNew struct {
	City         any `json:"city"`
	Temperatures any `json:"temperatures"`
}

func main() {
	data, err := os.ReadFile("temperature.json")
	if err != nil {
		fmt.Println("读取文件失败：", err)
		return
	}

	var cities []CityTemperatureNew
	if err := json.Unmarshal(data, &cities); err != nil {
		fmt.Println("JSON文件解析失败：", err)
		return
	}

	for _, city := range cities {
		cityName, ok := city.City.(string)
		if !ok {
			fmt.Println("city字段不是string类型，跳过该项。")
			continue
		}

		v := reflect.ValueOf(city.Temperatures)
		if v.Kind() != reflect.Slice {
			fmt.Printf("城市 %s 的 temperatures 字段不是数组，跳过。\n", cityName)
			continue
		}

		sum := 0.0
		count := v.Len()

		for i := 0; i < count; i++ {
			item := v.Index(i).Interface()
			f, ok := item.(float64)
			if !ok {
				fmt.Printf("城市 %s 的某个温度值不是 float64，跳过。\n", cityName)
				continue
			}
			sum += f
		}

		if count > 0 {
			avg := sum / float64(count)
			fmt.Printf("%s: %.2f\n", cityName, avg)
		}
	}
}
