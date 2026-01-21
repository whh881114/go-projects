# Go 基础：按数据类型的声明 / 初始化 / 赋值速查（简洁版）

> 记住一个底层事实：`var` 声明得到**零值**；`:=` 在函数内做**声明+初始化（类型推断）**；`=` 只是**赋值**。

---

## bool
```go
var b bool        // false
b = true

b2 := false       // 声明+初始化
```

---

## int / int8 / int16 / int32 / int64（以及 uint 系列）
```go
var i int         // 0
i = 42

i2 := 42          // 推断为 int
var i64 int64 = 42
i64 = int64(i)    // 需要显式转换
```

---

## float32 / float64
```go
var f float64     // 0.0
f = 3.14

f2 := 3.14        // 推断为 float64（默认）
var f32 float32 = 3.14
```

---

## complex64 / complex128
```go
var c complex128  // (0+0i)
c = 1 + 2i

c2 := complex(1.0, 2.0) // 也可以
```

---

## string
```go
var s string      // ""
s = "hello"

s2 := "world"
```

---

## byte / rune（别名：byte=uint8, rune=int32）
```go
var b byte = 'A'   // 65
var r rune = '你'  // Unicode code point

b2 := byte(255)
r2 := rune('😄')
```

---

## array（数组：长度固定，是值类型）
```go
var a [3]int        // [0 0 0]
a[0] = 1

a2 := [3]int{1, 2, 3}
a3 := [...]int{1, 2, 3} // 编译器推长度
```

---

## slice（切片：动态长度；nil 切片可 append）
```go
var s []int         // nil
s = append(s, 1, 2)

s2 := []int{1, 2, 3}
s3 := make([]int, 0, 10) // len=0 cap=10
s4 := make([]int, 3)     // len=3，已有元素 s4[0] 可直接赋值
s4[0] = 99
```

---

## map（哈希表：nil map 不能写）
```go
var m map[string]int // nil
// m["a"] = 1         // ❌ panic：nil map 不能写

m = make(map[string]int)
m["a"] = 1

m2 := map[string]int{"a": 1, "b": 2}
v := m2["a"]          // 读取不存在键会得到零值
v, ok := m2["x"]      // 推荐：区分“键不存在”
```

---

## pointer（指针：零值 nil）
```go
var p *int           // nil

x := 10
p = &x               // 取地址
*p = 20              // 解引用赋值，x 变成 20

p2 := new(int)       // *p2 == 0
*p2 = 7
```

---

## func（函数类型：零值 nil）
```go
var fn func(int) int // nil

fn = func(x int) int { return x + 1 }
y := fn(10)          // 11
```

---

## struct（结构体：字段有各自零值）
### 声明 / 初始化 / 字段赋值
```go
type User struct {
    Name string
    Age  int
}

var u User           // User{ "", 0 }
u.Name = "Tom"
u.Age = 18

u2 := User{Name: "Amy", Age: 20}
u3 := User{}         // 全零值
```

### struct 的 method（方法）声明方式

**值接收者（receiver 是值）**：不修改对象时常用（或对象很小）
```go
func (u User) Label() string {
    return u.Name
}
```

**指针接收者（receiver 是指针）**：需要修改对象 / 避免拷贝时常用
```go
func (u *User) Birthday() {
    u.Age++
}
```

调用时很省心：Go 会自动帮你做必要的取地址/解引用
```go
u := User{Name: "Tom", Age: 18}
u.Birthday()     // ✅ 即使 Birthday 是 *User 接收者，也能这样调用
label := u.Label()
```

---

## interface（接口：零值 nil；可以装任何实现了方法集的具体类型）
```go
type Greeter interface{ Hello() string }

var g Greeter        // nil

// 假设 type Person struct{} 实现了 Hello()
g = Person{}
_ = g.Hello()
```

---

## chan（通道：零值 nil；必须 make 才能用）
```go
var ch chan int      // nil
// ch <- 1            // ❌ 会阻塞（nil chan 发送/接收都永久阻塞）

ch = make(chan int)      // 无缓冲
ch2 := make(chan int, 1) // 有缓冲
```

---

## 赋值小抄（跨类型不自动转换）
```go
i := 1
f := float64(i)  // ✅ 必须显式转换
```

---

## 两个最常见坑（背下来）
1) `var s []int` 后不能直接 `s[0]=...`（len=0 会越界），要 `append` 或 `make([]int, 1)`
2) `var m map[K]V` 不能直接写 `m[k]=v`，要先 `make(map[K]V)`

