package main

import "fmt"

func generate(numbers chan int) {
	for i := 2; i <= 1000; i++ {
		numbers <- i
	}
	close(numbers)
}

func filter(in <-chan int, out chan<- int, prime int) {
	for num := range in {
		if num%prime != 0 {
			out <- num
		}
	}
	close(out)
}

func main() {
	numbers := make(chan int)
	go generate(numbers)

	for {
		prime, ok := <-numbers
		if !ok {
			break
		}
		fmt.Println(prime)

		// 创建下一个过滤器的输入通道
		filteredNumbers := make(chan int)
		go filter(numbers, filteredNumbers, prime)

		// 下一轮用筛选过的通道继续
		numbers = filteredNumbers
	}
}
