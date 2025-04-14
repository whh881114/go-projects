# Go 交叉编译
Golang 支持交叉编译，在一个平台上生成另一个平台的可执行程序。

## Mac 下编译 Linux 和 Windows 64位可执行程序
```shell
  CGO_ENABLED=0 
  GOOS=linux 
  GOARCH=amd64 
  go build main.go

  CGO_ENABLED=0 
  GOOS=windows 
  GOARCH=amd64 
  go build main.go
```

## Linux 下编译 Mac 和 Windows 64位可执行程序
```shell
  CGO_ENABLED=0 
  GOOS=darwin 
  GOARCH=amd64 
  go build main.go

  CGO_ENABLED=0 
  GOOS=windows 
  GOARCH=amd64 
  go build main.go
```

## Windows 下编译 Mac 和 Linux 64位可执行程序
```shell
  SET CGO_ENABLED=0
  SET GOOS=darwin
  SET GOARCH=amd64
  go build main.go
  
  SET CGO_ENABLED=0
  SET GOOS=linux
  SET GOARCH=amd64
  go build main.go
```