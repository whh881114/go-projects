# Go 常见“奇怪语法”速查表（收藏级）

---

## 🧠 基础类型对比：Array / Slice / Slice Pointer

| 写法       | 含义           | 说明                         |
|------------|----------------|------------------------------|
| `[5]int`   | 长度为 5 的数组 | 静态数组，大小固定           |
| `[]int`    | int 的切片     | 动态扩容                     |
| `*[]int`   | 指向切片的指针 | 传引用，需用 `&slice` 或 `new` 创建 |

---

## 🔹 Map 类型（重点）

| 写法                    | 含义                    | 说明                               |
|-------------------------|-------------------------|------------------------------------|
| `map[int]int`           | int -> int 映射         | 最基础的写法                       |
| `map[string][]string`   | string -> []string 映射 | 常用于 header、参数等              |
| `map[int]func() int`    | int -> 函数             | 函数无参返回 int，容易读错         |
| `map[int]*[]int`        | int -> 指向切片的指针   | 注意解引用才能操作                 |

---

## 🔹 函数类型 / 函数变量

| 写法                            | 含义                           | 说明                       |
|----------------------------------|--------------------------------|----------------------------|
| `func(int) int`                 | 参数 int 返回 int 的函数类型   | 常用于回调、参数、变量     |
| `f := func(x int) int { ... }`  | 匿名函数赋值给变量             | 可以直接调用 `f(3)`         |
| `type Handler func(int) error`  | 定义函数类型别名               | 增强可读性                 |

---

## 🔹 Channel

| 写法          | 含义                    | 说明                   |
|---------------|-------------------------|------------------------|
| `chan int`     | 可发送接收 int 的通道    | 双向通道               |
| `chan<- int`   | 只能发送 int 的通道      | 常用于函数参数         |
| `<-chan int`   | 只能接收 int 的通道      | 常用于只读参数         |

---

## 🔹 Struct / Struct 指针

| 写法                       | 含义              | 说明                           |
|----------------------------|-------------------|--------------------------------|
| `type Person struct {...}` | 定义结构体         | 基本写法                       |
| `&Person{Name: "Tom"}`     | 结构体指针初始化   | 返回 `*Person`，传引用常用     |

---

## 🔹 Interface（接口）

| 写法                     | 含义                           | 说明                            |
|--------------------------|--------------------------------|---------------------------------|
| `interface{}`            | 空接口，表示任意类型           | 相当于 Java/C++ 的 `Object`    |
| `interface { Do() }`     | 含有方法的接口定义             | Go 的鸭子类型基础              |

---

## 🔹 常见组合类型（眼花缭乱合集）

| 写法                             | 含义                                |
|----------------------------------|-------------------------------------|
| `map[int][]chan string`          | int -> 切片 -> chan string         |
| `[]map[string]int`               | 切片 -> 每个元素是 map             |
| `map[int]func() chan bool`       | int -> 返回 chan 的函数            |
| `map[int]*map[string]int`        | int -> 指向 map 的指针             |

---

## ✅ 建议用法

- 每天扫一眼，记住容易混淆的组合。
- 用 `type` 给复杂结构取个别名，增强可读性。
- 用 IDE 提示 + `fmt.Printf("%T\n", var)` 理解结构。

---

## 🧩 Bonus：手写推荐模板

```go
type IntList []int
type StringMap map[string]string
type Handler func(w http.ResponseWriter, r *http.Request)

type Person struct {
Name string
Age  int
}

type Service interface {
Start() error
Stop() error
}
```

---

## 🛠️ Go 中各种类型的声明方式与初始化写法

### 🔸 Slice 声明与初始化

```go
var s1 []int              // 声明但未初始化（nil）
s2 := []int{1, 2, 3}      // 直接初始化
s3 := make([]int, 5)      // 长度为 5 的切片，元素为 0
```

---

### 🔸 Map 声明与初始化

```go
var m1 map[string]int         // nil map，不能直接写入
m2 := map[string]int{}        // 空 map，可写入
m3 := make(map[string]int)    // 推荐写法，可写入
m4 := map[int][]int{
    1: {1, 2},
    2: {3, 4},
}
```

---

### 🔸 Struct 声明与初始化

```go
type Person struct {
    Name string
    Age  int
}

var p1 Person                         // 零值初始化
p2 := Person{"Alice", 30}            // 顺序初始化
p3 := Person{Name: "Bob", Age: 25}   // 字段名初始化
p4 := &Person{Name: "Eve"}           // 指针初始化
```

---

### 🔸 Channel 声明与初始化

```go
var ch1 chan int           // nil 通道，不能用
ch2 := make(chan int)      // 双向通道
ch3 := make(chan int, 10)  // 有缓冲通道
```

---

### 🔸 函数类型声明与赋值

```go
var f1 func(int) int
f1 = func(x int) int {
    return x * 2
}

type Handler func(string) error
var h Handler = func(s string) error {
    fmt.Println(s)
    return nil
}
```

---

### 🔸 Interface 声明与使用

```go
type Runner interface {
    Run() error
}

type Task struct{}
func (t Task) Run() error {
    return nil
}

var r Runner = Task{}     // 接口变量赋值
```
