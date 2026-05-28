FROM golang:1.24 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
RUN go get github.com/eclipse/paho.mqtt.golang
COPY . .
RUN go build -o /bot ./main.go

FROM ubuntu:22.04
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /root/
COPY --from=builder /bot .
EXPOSE 8080
CMD ["./bot"]
