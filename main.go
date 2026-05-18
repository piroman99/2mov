package main

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"
)

// User структура для хранения в MongoDB
type User struct {
    ID           string    `bson:"_id,omitempty"`
    MaxUserID    int       `bson:"max_user_id,omitempty"`
    TelegramID   int64     `bson:"telegram_id,omitempty"`
    FirstName    string    `bson:"first_name"`
    LastName     string    `bson:"last_name"`
    Username     string    `bson:"username"`
    Role         string    `bson:"role"`         // passenger, driver, courier
    Rating       float64   `bson:"rating"`
    TripsCount   int       `bson:"trips_count"`
    CreatedAt    time.Time `bson:"created_at"`
    LastActiveAt time.Time `bson:"last_active_at"`
}

var mongoClient *mongo.Client
var db *mongo.Database

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
    mongoClient = client
    db = mongoClient.Database("2mov")

    // Проверка подключения
    err = mongoClient.Ping(context.Background(), nil)
    if err != nil {
        log.Fatal("❌ MongoDB не отвечает:", err)
    }
    log.Println("✅ Подключение к MongoDB установлено")

    mode := os.Getenv("MODE")
    if mode == "" {
        mode = "max"
    }

    if mode == "telegram" {
        runTelegramBot()
    } else {
        runMaxBot()
    }
}

// findOrCreateUserByMaxID — поиск или создание пользователя по MaxUserID
func findOrCreateUserByMaxID(maxUserID int, firstName, lastName, username string) (*User, error) {
    collection := db.Collection("users")
    ctx := context.Background()

    var user User
    err := collection.FindOne(ctx, bson.M{"max_user_id": maxUserID}).Decode(&user)
    if err == nil {
        // Пользователь найден — обновляем last_active_at
        update := bson.M{"$set": bson.M{"last_active_at": time.Now()}}
        collection.UpdateOne(ctx, bson.M{"max_user_id": maxUserID}, update)
        return &user, nil
    }

    // Не найден — создаём нового
    newUser := User{
        MaxUserID:    maxUserID,
        FirstName:    firstName,
        LastName:     lastName,
        Username:     username,
        Role:         "passenger",
        Rating:       5.0,
        TripsCount:   0,
        CreatedAt:    time.Now(),
        LastActiveAt: time.Now(),
    }
    _, err = collection.InsertOne(ctx, newUser)
    if err != nil {
        return nil, err
    }
    return &newUser, nil
}

func runMaxBot() {
    log.Println("🤖 Запуск MAX-бота...")

    token := os.Getenv("MAX_BOT_TOKEN")
    if token == "" {
        log.Fatal("❌ MAX_BOT_TOKEN не задан")
    }

    http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
        body, err := io.ReadAll(r.Body)
        if err != nil {
            log.Printf("❌ Ошибка чтения тела: %v", err)
            w.WriteHeader(http.StatusBadRequest)
            return
        }
        defer r.Body.Close()

        if len(body) == 0 {
            log.Println("⚠️ Пустой вебхук (heartbeat), игнорируем")
            w.WriteHeader(http.StatusOK)
            return
        }

        log.Printf("📩 Получен вебхук: %s", string(body))

        var update map[string]interface{}
        if err := json.Unmarshal(body, &update); err != nil {
            log.Printf("❌ Ошибка парсинга JSON: %v", err)
            w.WriteHeader(http.StatusBadRequest)
            return
        }

        // Извлекаем текст сообщения
        var text string
        var maxUserID int
        var firstName, lastName, username string

        if msg, ok := update["message"].(map[string]interface{}); ok {
            if body, ok := msg["body"].(map[string]interface{}); ok {
                text, _ = body["text"].(string)
            }
            if sender, ok := msg["sender"].(map[string]interface{}); ok {
                if id, ok := sender["user_id"]; ok {
                    switch v := id.(type) {
                    case float64:
                        maxUserID = int(v)
                    case int:
                        maxUserID = v
                    }
                }
                firstName, _ = sender["first_name"].(string)
                lastName, _ = sender["last_name"].(string)
                username, _ = sender["username"].(string)
            }
        }

        if text == "" || maxUserID == 0 {
            log.Printf("⚠️ Нет текста (%s) или user_id (%d), игнорируем", text, maxUserID)
            w.WriteHeader(http.StatusOK)
            return
        }

        // Сохраняем или обновляем пользователя в БД
        user, err := findOrCreateUserByMaxID(maxUserID, firstName, lastName, username)
        if err != nil {
            log.Printf("❌ Ошибка работы с БД: %v", err)
        } else {
            log.Printf("👤 Пользователь: %s %s (ID: %d, рейтинг: %.1f)", user.FirstName, user.LastName, user.MaxUserID, user.Rating)
        }

        var reply string
        switch text {
        case "/start":
            reply = fmt.Sprintf("🚕 Добро пожаловать в 2MOV, %s!\nВаш рейтинг: %.1f\nОтправьте /help для списка команд", firstName, user.Rating)
	case "/profile":
	    reply = fmt.Sprintf("👤 %s %s\n⭐ Рейтинг: %.1f\n🚕 Поездок: %d\n👔 Роль: %s",
		user.FirstName, user.LastName, user.Rating, user.TripsCount, user.Role)
        case "/help":
            reply = "📋 Доступные команды:\n/start — начало\n/help — справка\n/profile — мой профиль"
        default:
            reply = "Отправьте /help для списка команд"
        }

        go sendMaxMessage(token, maxUserID, reply)
        w.WriteHeader(http.StatusOK)
    })

    go func() {
        log.Println("✅ HTTP-сервер запущен на :8080")
        if err := http.ListenAndServe(":8080", nil); err != nil {
            log.Printf("❌ Ошибка HTTP-сервера: %v", err)
        }
    }()

    log.Println("✅ MAX-бот готов к приёму вебхуков")

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    log.Println("👋 MAX-бот остановлен")
}

func sendMaxMessage(token string, recipientID int, text string) {
    url := fmt.Sprintf("https://platform-api.max.ru/messages?user_id=%d", recipientID)

    payload := map[string]interface{}{
        "text": text,
    }
    jsonData, err := json.Marshal(payload)
    if err != nil {
        log.Printf("❌ Ошибка маршалинга JSON: %v", err)
        return
    }

    req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
    if err != nil {
        log.Printf("❌ Ошибка создания запроса: %v", err)
        return
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", token)

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        log.Printf("❌ Ошибка отправки сообщения: %v", err)
        return
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    log.Printf("📤 Ответ MAX API (status=%d): %s", resp.StatusCode, string(body))
}

func runTelegramBot() {
    log.Println("🤖 Запуск Telegram-бота...")
    log.Println("✅ Telegram-бот готов (заглушка)")
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    log.Println("👋 Telegram-бот остановлен")
}
