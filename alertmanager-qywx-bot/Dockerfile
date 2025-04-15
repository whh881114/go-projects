FROM golang:1.24.2

WORKDIR /app
COPY main.go .

RUN go build -o alertmanager-qywx-bot main.go

CMD ["./alertmanager-qywx-bot"]
