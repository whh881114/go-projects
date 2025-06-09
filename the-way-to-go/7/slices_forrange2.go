package main

import "fmt"

func main() {
	seasons := []string{"Spring", "Summer", "Autum", "Winter"}
	for i, s := range seasons {
		fmt.Println(i, s)
	}

	var season string
	for _, season = range seasons {
		fmt.Println(season)
	}
}
