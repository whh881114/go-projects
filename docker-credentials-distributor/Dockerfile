FROM golang:1.24.2

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod tidy

COPY main.go ./
RUN go build -o docker-credentials-distributor main.go

ENTRYPOINT ["./docker-credentials-distributor"]
