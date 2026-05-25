package main

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "math"
    "net/http"
    "os"
    "os/signal"
    "strings"
    "syscall"
    "time"

    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/bson/primitive"
    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"
)

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
    Step        string    `bson:"step"`
    FromAddress string    `bson:"from_address"`
    FromLat     float64   `bson:"from_lat"`
    FromLon     float64   `bson:"from_lon"`
    ToAddress   string    `bson:"to_address"`
    ToLat       float64   `bson:"to_lat"`
    ToLon       float64   `bson:"to_lon"`
    UpdatedAt   time.Time `bson:"updated_at"`
}

type Order struct {
    ID          string    `bson:"_id,omitempty"`
    ClientID    string    `bson:"client_id"`
    FromAddress string    `bson:"from_address"`
    FromLat     float64   `bson:"from_lat"`
    FromLon     float64   `bson:"from_lon"`
    ToAddress   string    `bson:"to_address"`
    ToLat       float64   `bson:"to_lat"`
    ToLon       float64   `bson:"to_lon"`
    Price       float64   `bson:"price"`
    Status      string    `bson:"status"`
    DriverID    string    `bson:"driver_id,omitempty"`
    CreatedAt   time.Time `bson:"created_at"`
}

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
    order.Price = calculatePrice(simpleDistance(order.FromLat, order.FromLon, order.ToLat, order.ToLon))
    _, err := collection.InsertOne(context.Background(), order)
    return err
}

func simpleDistance(lat1, lon1, lat2, lon2 float64) float64 {
    const earthRadius = 6371.0
    dLat := (lat2 - lat1) * math.Pi / 180
    dLon := (lon2 - lon1) * math.Pi / 180
    a := math.Sin(dLat/2)*math.Sin(dLat/2) +
        math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
            math.Sin(dLon/2)*math.Sin(dLon/2)
    c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
    return earthRadius * c
}

func calculatePrice(distance float64) float64 {
    basePrice := 200.0
    pricePerKm := 30.0
    return basePrice + distance*pricePerKm
}

func parseLocation(update map[string]interface{}) (address string, lat, lon float64, err error) {
    msg, ok := update["message"].(map[string]interface{})
    if !ok {
        return "", 0, 0, fmt.Errorf("no message")
    }
    if body, ok := msg["body"].(map[string]interface{}); ok {
        if attachments, ok := body["attachments"].([]interface{}); ok && len(attachments) > 0 {
            for _, att := range attachments {
                if attMap, ok := att.(map[string]interface{}); ok {
                    if attMap["type"] == "location" {
                        lat, _ = attMap["latitude"].(float64)
                        lon, _ = attMap["longitude"].(float64)
                        return fmt.Sprintf("%f,%f", lat, lon), lat, lon, nil
                    }
                }
            }
        }
        if text, ok := body["text"].(string); ok && text != "" {
            return text, 0, 0, nil
        }
    }
    return "", 0, 0, fmt.Errorf("no location")
}

func sendMaxMessage(token, chatID, text string) {
    url := fmt.Sprintf("https://platform-api.max.ru/messages?user_id=%s", chatID)
    payload := map[string]interface{}{"text": text}
    jsonData, _ := json.Marshal(payload)
    req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", token)
    http.DefaultClient.Do(req)
}

func sendMaxMessageWithButtons(token, chatID, text string, buttons [][]map[string]interface{}) {
    url := fmt.Sprintf("https://platform-api.max.ru/messages?user_id=%s", chatID)
    payload := map[string]interface{}{
        "text": text,
        "attachments": []map[string]interface{}{
            {
                "type": "inline_keyboard",
                "payload": map[string]interface{}{
                    "buttons": buttons,
                },
            },
        },
    }
    jsonData, _ := json.Marshal(payload)
    req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", token)
    http.DefaultClient.Do(req)
}

func sendCallbackAnswer(token, callbackID, notification, newText string, updateMessage bool) {
    url := fmt.Sprintf("https://platform-api.max.ru/answers?callback_id=%s", callbackID)
    answerBody := map[string]interface{}{
        "notification": notification,
    }
    if updateMessage && newText != "" {
        answerBody["message"] = map[string]interface{}{
            "text": newText,
        }
    }
    jsonData, _ := json.Marshal(answerBody)
    req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", token)
    http.DefaultClient.Do(req)
}

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
    token := os.Getenv("MAX_BOT_TOKEN")
    if token == "" {
        log.Fatal("❌ MAX_BOT_TOKEN не задан")
    }

    http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
        body, err := io.ReadAll(r.Body)
        if err != nil {
            w.WriteHeader(http.StatusBadRequest)
            return
        }
        defer r.Body.Close()
        if len(body) == 0 {
            w.WriteHeader(http.StatusOK)
            return
        }

        var update map[string]interface{}
        if err := json.Unmarshal(body, &update); err != nil {
            w.WriteHeader(http.StatusBadRequest)
            return
        }

        // Обработка callback-кнопок
        var cb map[string]interface{}
        var ok bool

        if cb, ok = update["callback"].(map[string]interface{}); !ok {
            cb, ok = update["message_callback"].(map[string]interface{})
        }

        if ok {
            callbackID := cb["callback_id"].(string)
            payload := cb["payload"].(string)

            var userID string
            if userObj, ok := cb["user"].(map[string]interface{}); ok {
                if id, ok := userObj["user_id"]; ok {
                    userID = fmt.Sprintf("%.0f", id.(float64))
                }
            }

            log.Printf("🔘 Callback: userID=%s, payload=%s", userID, payload)

            if payload == "confirm_order" {
                session, err := getSession(userID)
                if err != nil {
                    sendCallbackAnswer(token, callbackID, "❌ Сессия устарела", "Начните заказ заново с /order", true)
                    w.WriteHeader(http.StatusOK)
                    return
                }
                order := Order{
                    ClientID:    userID,
                    FromAddress: session.FromAddress,
                    FromLat:     session.FromLat,
                    FromLon:     session.FromLon,
                    ToAddress:   session.ToAddress,
                    ToLat:       session.ToLat,
                    ToLon:       session.ToLon,
                }
                saveOrder(order)
                deleteSession(userID)
                sendCallbackAnswer(token, callbackID, "✅ Заказ создан! Ищем водителя...", "✅ Заказ создан! Ищем водителя...", true)
            } else if payload == "edit_order" {
                deleteSession(userID)
                sendCallbackAnswer(token, callbackID, "❌ Заказ отменён", "❌ Заказ отменён. Начните заново с /order", true)
            }

            w.WriteHeader(http.StatusOK)
            return
        }

        // Обычная обработка
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

        if maxUserID == 0 {
            w.WriteHeader(http.StatusOK)
            return
        }

        user, _ := findOrCreateUserByMaxID(maxUserID, firstName, lastName, username)
        userIDStr := fmt.Sprintf("%d", maxUserID)

        // ========== ОТПРАВКА СООБЩЕНИЯ ВОДИТЕЛЮ ==========
        if text != "" && !strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "💬") {
            var chat struct {
                DriverID string `bson:"driver_id"`
                ClientID string `bson:"client_id"`
                Status   string `bson:"status"`
            }
            err := db.Collection("chats").FindOne(context.Background(),
                bson.M{"client_id": userIDStr, "status": "active"}).Decode(&chat)
            if err == nil {
                msg := bson.M{
                    "order_id":   "",
                    "from_user":  "client_" + userIDStr,
                    "to_user":    "driver_" + chat.DriverID,
                    "text":       text,
                    "status":     "pending",
                    "created_at": time.Now(),
                }
                db.Collection("chat_messages").InsertOne(context.Background(), msg)
                sendMaxMessage(token, userIDStr, "✅ Сообщение отправлено водителю")
                w.WriteHeader(http.StatusOK)
                return
            }
        }

        switch text {
        case "/start":
            reply := fmt.Sprintf("🚕 Добро пожаловать в 2MOV, %s!\nВаш рейтинг: %.1f\nОтправьте /help", firstName, user.Rating)
            sendMaxMessage(token, userIDStr, reply)
        case "/help":
            reply := "📋 Доступные команды:\n/start — начало\n/help — справка\n/profile — мой профиль\n/order — создать заказ"
            sendMaxMessage(token, userIDStr, reply)
        case "/profile":
            reply := fmt.Sprintf("👤 %s %s\n⭐ Рейтинг: %.1f\n🚕 Поездок: %d", user.FirstName, user.LastName, user.Rating, user.TripsCount)
            sendMaxMessage(token, userIDStr, reply)
        case "/order":
            session := Session{UserID: userIDStr, Step: "from", UpdatedAt: time.Now()}
            saveSession(session)
            sendMaxMessage(token, userIDStr, "📍 Отправьте точку отправления (геолокацию или адрес)")
        default:
            session, err := getSession(userIDStr)
            if err == nil {
                switch session.Step {
                case "from":
                    addr, lat, lon, err := parseLocation(update)
                    if err != nil {
                        sendMaxMessage(token, userIDStr, "Не удалось определить адрес. Попробуйте ещё раз или отправьте геолокацию.")
                        break
                    }
                    session.FromAddress = addr
                    session.FromLat = lat
                    session.FromLon = lon
                    session.Step = "to"
                    saveSession(session)
                    sendMaxMessage(token, userIDStr, "📍 Отправьте точку назначения (геолокацию или адрес)")
                case "to":
                    addr, lat, lon, err := parseLocation(update)
                    if err != nil {
                        sendMaxMessage(token, userIDStr, "Не удалось определить адрес. Попробуйте ещё раз.")
                        break
                    }
                    session.ToAddress = addr
                    session.ToLat = lat
                    session.ToLon = lon
                    session.Step = "confirm"
                    saveSession(session)
                    distance := simpleDistance(session.FromLat, session.FromLon, session.ToLat, session.ToLon)
                    price := calculatePrice(distance)
                    reply := fmt.Sprintf("🚚 Заказ:\n📍 Откуда: %s\n📍 Куда: %s\n📏 Расстояние: %.1f км\n💰 Цена: %.0f ₽\n\nПодтверждаете заказ?", session.FromAddress, session.ToAddress, distance, price)
                    buttons := [][]map[string]interface{}{
                        {
                            {
                                "type": "callback",
                                "text": "✅ Да",
                                "payload": "confirm_order",
                            },
                            {
                                "type": "callback",
                                "text": "✏️ Изменить",
                                "payload": "edit_order",
                            },
                        },
                    }
                    sendMaxMessageWithButtons(token, userIDStr, reply, buttons)
                default:
                    sendMaxMessage(token, userIDStr, "Отправьте /help для списка команд")
                }
            } else {
                sendMaxMessage(token, userIDStr, "Отправьте /help для списка команд")
            }
        }

        w.WriteHeader(http.StatusOK)
    })

    go func() {
        log.Println("✅ HTTP-сервер запущен на :8080")
        if err := http.ListenAndServe(":8080", nil); err != nil {
            log.Printf("❌ Ошибка HTTP-сервера: %v", err)
        }
    }()

    // ========== ФОНОВАЯ ПРОВЕРКА СООБЩЕНИЙ ==========
    go func() {
        ticker := time.NewTicker(3 * time.Second)
        for range ticker.C {
            var chats []struct {
                ClientID string `bson:"client_id"`
            }
            cursor, err := db.Collection("chats").Find(context.Background(), bson.M{"status": "active"})
            if err != nil {
                continue
            }
            cursor.All(context.Background(), &chats)

            for _, chat := range chats {
                var messages []bson.M
                msgCursor, err := db.Collection("chat_messages").Find(context.Background(),
                    bson.M{"to_user": "client_" + chat.ClientID, "status": "pending"})
                if err != nil {
                    continue
                }
                msgCursor.All(context.Background(), &messages)

                for _, msg := range messages {
                    text := msg["text"].(string)
                    sendMaxMessage(token, chat.ClientID, fmt.Sprintf("💬 %s", text))
                    db.Collection("chat_messages").UpdateOne(context.Background(),
                        bson.M{"_id": msg["_id"]},
                        bson.M{"$set": bson.M{"status": "delivered", "delivered_at": time.Now()}})
                }
            }
        }
    }()

    log.Println("✅ MAX-бот готов к приёму вебхуков")
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    log.Println("👋 MAX-бот остановлен")
}
