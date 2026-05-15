package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"

    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"
)

type User struct {
    ID       string `bson:"_id"`
    Username string `bson:"username"`
    Platform string `bson:"platform"` // "max" or "telegram"
}

func main() {
    log.Println("🚀 2MOV бот запускается...")

    // Подключение к MongoDB
    mongoURI := os.Getenv("MONGO_URI")
    if mongoURI == "" {
        mongoURI = "mongodb://localhost:27017"
    }

    client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(mongoURI))
    if err != nil {
        log.Fatal("❌ Ошибка подключения к MongoDB:", err)
    }
    defer client.Disconnect(context.Background())

    log.Println("✅ Подключение к MongoDB установлено")

    // TODO: здесь будет код для MAX и Telegram ботов
    log.Println("⏳ Обработчики команд в разработке...")

    // Ожидание сигнала завершения
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    log.Println("👋 2MOV бот остановлен")
}
