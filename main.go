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
    "go.mongodb.org/mongo-driver/bson/primitive"
    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"
)

// ========== MODELS ==========

type User struct {
    ID           string    `bson:"_id,omitempty"`
    MaxUserID    int       `bson:"max_user_id,omitempty"`
    FirstName    string    `bson:"first_name"`
    LastName     string    `bson:"last_name"`
    Username     string    `bson:"username"`
    Role         string    `bson:"role"`
    Rating       float64   `bson:"rating"`
    TripsCount   int       `bson:"trips_count"`
    CreatedAt    time.Time `bson:"created_at"`
    LastActiveAt time.Time `bson:"last_active_at"`
}

type Session struct {
    UserID      string    `bson:"user_id"`
    Step        string    `bson:"step"` // from, to, confirm
    FromAddress string    `bson:"from_address"`
    ToAddress   string    `bson:"to_address"`
    UpdatedAt   time.Time `bson:"updated_at"`
}

type Order struct {
    ID          string    `bson:"_id,omitempty"`
    ClientID    string    `bson:"client_id"`
    FromAddress string    `bson:"from_address"`
    ToAddress   string    `bson:"to_address"`
    Price       float64   `bson:"price"`
    Status      string    `bson:"status"`
    CreatedAt   time.Time `bson:"created_at"`
}

// ========== STORAGE ==========

var mongoClient *mongo.Client
var db *mongo.Database

func findOrCreateUserByMaxID(maxUserID int, firstName, lastName, username string) (*User, error) {
    collection := db.Collection("users")
    ctx := context.Background()

    var user User
    err := collection.FindOne(ctx, bson.M{"max_user_id": maxUserID}).Decode(&user)
    if err == nil {
        update := bson.M{"$set": bson.M{"last_active_at": time.Now()}}
        collection.UpdateOne(ctx, bson.M{"max_user_id": maxUserID}, update)
        return &user, nil
    }

    newUser := User{
        MaxUserID:    maxUserID,
        FirstName:    firstName,
        LastName:     lastName,
        Username:     username,
        Role:         "client",
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

func saveSession(s Session) {
    collection := db.Collection("sessions")
    opts := options.Update().SetUpsert(true)
    filter := bson.M{"user_id": s.UserID}
    update := bson.M{"$set": s}
    collection.UpdateOne(context.Background(), filter, update, opts)
}

func getSession(userID string) (Session, error) {
    var session Session
    collection := db.Collection("sessions")
    err := collection.FindOne(context.Background(), bson.M{"user_id": userID}).Decode(&session)
    return session, err
}

func deleteSession(userID string) {
    collection := db.Collection("sessions")
    collection.DeleteOne(context.Background(), bson.M{"user_id": userID})
}

func saveOrder(order Order) error {
    collection := db.Collection("orders")
    order.ID = primitive.NewObjectID().Hex()
    order.CreatedAt = time.Now()
    order.Status = "pending"
    order.Price = calculatePrice(simpleDistance()) // заглушка
    _, err := collection.InsertOne(context.Background(), order)
    return err
}

// ========== HELPERS ==========

func simpleDistance() float64 {
    // TODO: заменить на реальное расстояние через 2GIS
    return 5.0
}

func calculatePrice(distance float64) float64 {
    basePrice := 200.0
    pricePerKm := 30.0
    return basePrice + distance*pricePerKm
}

func sendMaxMessage(token, chatID, text string) {
    url := fmt.Sprintf("https://platform-api.max.ru/messages?user_id=%s", chatID)

    payload := map[string]interface{}{
        "text": text,
    }
    jsonData, _ := json.Marshal(payload)

    req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", token)

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        log.Printf("❌ Ошибка отправки сообщения: %v", err)
        return
    }
    defer resp.Body.Close()
}

// ========== MAIN ==========

func main() {
    log.Println("🚀 2MOV бот запускается...")

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

    err = mongoClient.Ping(context.Background(), nil)
    if err != nil {
        log.Fatal("❌ MongoDB не отвечает:", err)
    }
    log.Println("✅ Подключение к MongoDB установлено")

    runMaxBot()
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

        // Извлекаем текст и user_id
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
            log.Printf("⚠️ Нет текста или user_id, игнорируем")
            w.WriteHeader(http.StatusOK)
            return
        }

        user, err := findOrCreateUserByMaxID(maxUserID, firstName, lastName, username)
        if err != nil {
            log.Printf("❌ Ошибка работы с БД: %v", err)
        } else {
            log.Printf("👤 Пользователь: %s %s (ID: %d, рейтинг: %.1f)", user.FirstName, user.LastName, user.MaxUserID, user.Rating)
        }

        var reply string

        // Обработка команд
        if text == "/start" {
            reply = fmt.Sprintf("🚕 Добро пожаловать в 2MOV, %s!\nВаш рейтинг: %.1f\nОтправьте /help для списка команд", firstName, user.Rating)
        } else if text == "/help" {
            reply = "📋 Доступные команды:\n/start — начало\n/help — справка\n/profile — мой профиль\n/order — создать заказ"
        } else if text == "/profile" {
            reply = fmt.Sprintf("👤 %s %s\n⭐ Рейтинг: %.1f\n🚕 Поездок: %d", user.FirstName, user.LastName, user.Rating, user.TripsCount)
        } else if text == "/order" {
            session := Session{
                UserID:      fmt.Sprintf("%d", maxUserID),
                Step:        "from",
                UpdatedAt:   time.Now(),
            }
            saveSession(session)
            reply = "📍 Отправьте адрес отправления текстом (например, ул. Ленина, 10)"
        } else {
            // Обработка сессии (шаги заказа)
            session, err := getSession(fmt.Sprintf("%d", maxUserID))
            if err == nil {
                switch session.Step {
                case "from":
                    session.FromAddress = text
                    session.Step = "to"
                    saveSession(session)
                    reply = "📍 Отправьте адрес назначения"

                case "to":
                    session.ToAddress = text
                    session.Step = "confirm"
                    saveSession(session)

                    distance := simpleDistance()
                    price := calculatePrice(distance)

                    reply = fmt.Sprintf(
                        "🚚 Заказ:\nОткуда: %s\nКуда: %s\nРасстояние: %.1f км\nЦена: %.0f ₽\n\nПодтверждаете?\n1 — Да\n2 — Отмена",
                        session.FromAddress, session.ToAddress, distance, price,
                    )

                case "confirm":
                    if text == "1" {
                        order := Order{
                            ClientID:    fmt.Sprintf("%d", maxUserID),
                            FromAddress: session.FromAddress,
                            ToAddress:   session.ToAddress,
                        }
                        saveOrder(order)
                        deleteSession(fmt.Sprintf("%d", maxUserID))
                        reply = "✅ Заказ создан! Ищем водителя..."
                    } else {
                        deleteSession(fmt.Sprintf("%d", maxUserID))
                        reply = "❌ Заказ отменён"
                    }
                default:
                    reply = "Отправьте /help для списка команд"
                }
            } else {
                reply = "Отправьте /help для списка команд"
            }
        }

        go sendMaxMessage(token, fmt.Sprintf("%d", maxUserID), reply)
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
