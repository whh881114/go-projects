FROM golang:1.24.2

WORKDIR /app

COPY go.mod go.sum main.go ./

RUN go mod tidy
RUN go build -o docker-credentials-distributor main.go

ENTRYPOINT ["./docker-credentials-distributor"]
