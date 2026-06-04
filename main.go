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
    "strings"
    "syscall"
    "time"

    mqtt "github.com/eclipse/paho.mqtt.golang"
    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"
    "2mov/internal/constants"
    "2mov/internal/models"
    "2mov/internal/mymqtt"
    "2mov/internal/repository"
    "2mov/internal/utils"
)

var db *mongo.Database

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
    db = client.Database("2mov")
    err = db.Client().Ping(context.Background(), nil)
    if err != nil {
        log.Fatal("❌ MongoDB не отвечает:", err)
    }
    log.Println("✅ Подключение к MongoDB установлено")

    mymqtt.Init()
    runMaxBot()
}

func runMaxBot() {
    token := os.Getenv("MAX_BOT_TOKEN")
    if token == "" {
        log.Fatal("❌ MAX_BOT_TOKEN не задан")
    }

    adminUsername := os.Getenv("ADMIN_USERNAME")
    adminPassword := os.Getenv("ADMIN_PASSWORD")
    if adminUsername == "" {
        adminUsername = "admin"
    }
    if adminPassword == "" {
        adminPassword = "admin123"
    }

    // Установка подсказок команд
    go func() {
        commands := `{"commands":[
            {"name":"start","description":"Регистрация и приветствие"},
            {"name":"order","description":"Создать заказ на доставку"},
            {"name":"help","description":"Справка по командам"},
            {"name":"profile","description":"Мой профиль (рейтинг, поездки)"}
        ]}`
        req, _ := http.NewRequest("PATCH", "https://platform-api.max.ru/me", bytes.NewBufferString(commands))
        req.Header.Set("Authorization", token)
        req.Header.Set("Content-Type", "application/json")
        if resp, err := http.DefaultClient.Do(req); err == nil {
            defer resp.Body.Close()
            log.Printf("✅ Подсказки команд установлены (status=%d)", resp.StatusCode)
        } else {
            log.Printf("⚠️ Не удалось установить подсказки: %v", err)
        }
    }()

    // Админка
    http.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
        user, pass, ok := r.BasicAuth()
        if !ok || user != adminUsername || pass != adminPassword {
            w.Header().Set("WWW-Authenticate", `Basic realm="2MOV Admin"`)
            w.WriteHeader(http.StatusUnauthorized)
            return
        }

        ordersCollection := db.Collection("orders")
        ordersCursor, _ := ordersCollection.Find(context.Background(), bson.M{})
        var orders []models.Order
        ordersCursor.All(context.Background(), &orders)

        usersCollection := db.Collection("users")
        usersCursor, _ := usersCollection.Find(context.Background(), bson.M{})
        var users []models.User
        usersCursor.All(context.Background(), &users)

        w.Header().Set("Content-Type", "text/html")
        fmt.Fprintf(w, `<!DOCTYPE html>
        <html>
        <head><title>2MOV Admin</title><meta charset="UTF-8"></head>
        <body style="font-family: monospace; font-size: 14px;">
            <h1>2MOV Admin</h1>
            <p><a href="/admin/drivers">🚕 Водители</a></p>
            <h2>Заказы (%d)</h2>
            <table border="1" cellpadding="5">
                <tr><th>ID</th><th>Клиент</th><th>Водитель</th><th>Откуда</th><th>Куда</th><th>Цена</th><th>Статус</th><th>Создан</th></tr>
        `, len(orders))

        for _, o := range orders {
            fmt.Fprintf(w, `<tr>
                <td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%.0f</td><td>%s</td><td>%s</td>
            </tr>`, o.ID[:8], o.ClientID, o.DriverID, o.FromAddress, o.ToAddress, o.Price, o.Status, o.CreatedAt.Format("02.01 15:04"))
        }

        fmt.Fprintf(w, `</table>
            <h2>Пользователи (%d)</h2>
            <table border="1" cellpadding="5">
                <tr><th>ID</th><th>Имя</th><th>Роль</th><th>Рейтинг</th><th>Поездок</th><th>Активен</th></tr>
        `, len(users))

        for _, u := range users {
            userID := fmt.Sprintf("%d", u.MaxUserID)
            if userID == "0" && u.TelegramID != "" {
                userID = u.TelegramID
            }
            fmt.Fprintf(w, `<tr>
                <td>%s</td><td>%s %s</td><td>%s</td><td>%.1f</td><td>%d</td><td>%s</td>
            </tr>`, userID, u.FirstName, u.LastName, u.Role, u.Rating, u.TripsCount, u.LastActiveAt.Format("02.01 15:04"))
        }
        fmt.Fprintf(w, `<tr></body></html>`)
    })

    http.HandleFunc("/admin/drivers", func(w http.ResponseWriter, r *http.Request) {
        user, pass, ok := r.BasicAuth()
        if !ok || user != adminUsername || pass != adminPassword {
            w.Header().Set("WWW-Authenticate", `Basic realm="2MOV Admin"`)
            w.WriteHeader(http.StatusUnauthorized)
            return
        }

        usersCollection := db.Collection("users")
        cursor, _ := usersCollection.Find(context.Background(), bson.M{})
        var users []models.User
        cursor.All(context.Background(), &users)

        w.Header().Set("Content-Type", "text/html")
        fmt.Fprintf(w, `<!DOCTYPE html>
        <html>
        <head><title>2MOV Водители</title><meta charset="UTF-8"></head>
        <body>
            <h1>🚕 Водители</h1>
            <p><a href="/admin">← Назад к заказам</a></p>
            <table border="1">
                <tr><th>ID</th><th>Имя</th><th>Роль</th><th>Рейтинг</th><th>Поездок</th><th>Активен</th></tr>
        `)
        for _, u := range users {
            role := u.Role
            if role == "" {
                role = "client"
            }
            fmt.Fprintf(w, `<tr>
                <td>%d</td><td>%s %s</td><td>%s</td><td>%.1f</td><td>%d</td><td>%s</td>
            </tr>`, u.MaxUserID, u.FirstName, u.LastName, role, u.Rating, u.TripsCount, u.LastActiveAt.Format("02.01 15:04"))
        }
        fmt.Fprintf(w, `</table></body></html>`)
    })

    // MQTT подписки
    if mymqtt.IsConnected() {
        mymqtt.Client.Subscribe("chat/+", 1, func(c mqtt.Client, msg mqtt.Message) {
            parts := strings.Split(msg.Topic(), "/")
            if len(parts) == 2 {
                clientID := parts[1]
		log.Printf("🔁 Клиент получил сообщение (count)")
                log.Printf("📡 MQTT received for client %s: %s", clientID, msg.Payload())
                utils.SendMessage(token, clientID, string(msg.Payload()))
            }
        })
        log.Println("✅ MQTT chat subscription added")

        mymqtt.Client.Subscribe("status/+", 1, func(c mqtt.Client, msg mqtt.Message) {
            parts := strings.Split(msg.Topic(), "/")
            if len(parts) == 2 {
                clientID := parts[1]
                status := string(msg.Payload())
                text := getStatusText(status)
                if text != "" {
                    utils.SendMessage(token, clientID, text)
                }
            }
        })
        log.Println("✅ MQTT status subscription added")
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

        // Обработка callback
        if cb, ok := update["callback"].(map[string]interface{}); ok {
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
                session, err := repository.GetSession(db, userID)
                if err != nil {
                    sendCallbackAnswer(token, callbackID, "❌ Сессия устарела", "Начните заказ заново с /order", true)
                    w.WriteHeader(http.StatusOK)
                    return
                }
                distance := utils.SimpleDistance(session.FromLat, session.FromLon, session.ToLat, session.ToLon)
                price := utils.CalculatePrice(distance)
                order := models.Order{
                    ClientID:    userID,
                    FromAddress: session.FromAddress,
                    FromLat:     session.FromLat,
                    FromLon:     session.FromLon,
                    ToAddress:   session.ToAddress,
                    ToLat:       session.ToLat,
                    ToLon:       session.ToLon,
                    Price:       price,
                }
                repository.SaveOrder(db, order)
                repository.DeleteSession(db, userID)
                sendCallbackAnswer(token, callbackID, "✅ Заказ создан! Ищем водителя...", "✅ Заказ создан! Ищем водителя...", true)

                buttons := [][]map[string]interface{}{
                    {{"type": "callback", "text": "❌ Отменить заказ", "payload": "cancel_order"}},
                }
                utils.SendMessageWithButtons(token, userID, "✅ Заказ создан! Ищем водителя...", buttons)
            } else if payload == "edit_order" {
                repository.DeleteSession(db, userID)
                sendCallbackAnswer(token, callbackID, "❌ Заказ отменён", "❌ Заказ отменён. Начните заново с /order", true)
            } else if payload == "cancel_order" {
                cancelOrderByClient(token, userID)
            }

            w.WriteHeader(http.StatusOK)
            return
        }

        // Обычные сообщения
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

        user, err := repository.FindOrCreateUserByMaxID(db, maxUserID, firstName, lastName, username)
        if err != nil {
            log.Printf("⚠️ Ошибка создания/поиска пользователя: %v", err)
            w.WriteHeader(http.StatusOK)
            return
        }
        userIDStr := fmt.Sprintf("%d", maxUserID)

        // Отправка сообщения водителю
        if text != "" && !strings.HasPrefix(text, "/") && !strings.HasPrefix(text, "💬") {
            var chat struct {
                DriverID string `bson:"driver_id"`
                ClientID string `bson:"client_id"`
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

                if mymqtt.IsConnected() {
                    topic := "chat/" + chat.DriverID
                    mymqtt.Publish(topic, 1, false, text)
                    log.Printf("📡 MQTT publish to %s: %s", topic, text)
                }

                utils.SendMessage(token, userIDStr, "✅ Сообщение отправлено водителю")
                w.WriteHeader(http.StatusOK)
                return
            }
        }

        // Обработка команд
        switch text {
        case "/start":
            reply := constants.TestModeNotice + fmt.Sprintf("🚕 Добро пожаловать в 2MOV, %s!\nВаш рейтинг: %.1f\nОтправьте /help", firstName, user.Rating) + constants.HelpFooter
            utils.SendMessage(token, userIDStr, reply)
        case "/help":
            reply := constants.TestModeNotice + "📋 Доступные команды:\n/start — начало\n/help — справка\n/profile — мой профиль\n/order — создать заказ" + constants.HelpFooter
            utils.SendMessage(token, userIDStr, reply)
        case "/profile":
            reply := fmt.Sprintf("👤 %s %s\n⭐ Рейтинг: %.1f\n🚕 Поездок: %d", user.FirstName, user.LastName, user.Rating, user.TripsCount)
            utils.SendMessage(token, userIDStr, reply)
        case "/order":
            session := models.Session{UserID: userIDStr, Step: "from", UpdatedAt: time.Now()}
            repository.SaveSession(db, session)
            utils.SendMessage(token, userIDStr, "📍 Отправьте точку отправления (геолокацию или адрес)")
        default:
            session, err := repository.GetSession(db, userIDStr)
            if err == nil {
                switch session.Step {
                case "from":
                    addr, lat, lon, err := utils.ParseLocation(update)
                    if err != nil {
                        utils.SendMessage(token, userIDStr, "Не удалось определить адрес. Попробуйте ещё раз или отправьте геолокацию.")
                        break
                    }
                    session.FromAddress = addr
                    session.FromLat = lat
                    session.FromLon = lon
                    session.Step = "to"
                    repository.SaveSession(db, session)
                    utils.SendMessage(token, userIDStr, "📍 Отправьте точку назначения (геолокацию или адрес)")
                case "to":
                    addr, lat, lon, err := utils.ParseLocation(update)
                    if err != nil {
                        utils.SendMessage(token, userIDStr, "Не удалось определить адрес. Попробуйте ещё раз.")
                        break
                    }
                    session.ToAddress = addr
                    session.ToLat = lat
                    session.ToLon = lon
                    session.Step = "confirm"
                    repository.SaveSession(db, session)
                    distance := utils.SimpleDistance(session.FromLat, session.FromLon, session.ToLat, session.ToLon)
                    price := utils.CalculatePrice(distance)
                    reply := fmt.Sprintf("🚚 Заказ:\n📍 Откуда: %s\n📍 Куда: %s\n📏 Расстояние: %.1f км\n💰 Цена: %.0f ₽\n\nПодтверждаете?", session.FromAddress, session.ToAddress, distance, price)
                    buttons := [][]map[string]interface{}{
                        {
                            {"type": "callback", "text": "✅ Да", "payload": "confirm_order"},
                            {"type": "callback", "text": "✏️ Изменить", "payload": "edit_order"},
                        },
                    }
                    utils.SendMessageWithButtons(token, userIDStr, reply, buttons)
                default:
                    utils.SendMessage(token, userIDStr, "Отправьте /help для списка команд")
                }
            } else {
                utils.SendMessage(token, userIDStr, "Отправьте /help для списка команд")
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

    log.Println("✅ MAX-бот готов к приёму вебхуков")
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    log.Println("👋 MAX-бот остановлен")
}

// ----------------------------------------------------------------
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ (оставшиеся)
// ----------------------------------------------------------------

func getStatusText(status string) string {
    switch status {
    case "accepted":
        return "✅ Водитель принял ваш заказ!"
    case "at_pickup":
        return "📍 Водитель на месте забора"
    case "to_delivery":
        return "🚚 Водитель выехал на доставку"
    case "at_delivery":
        return "📍 Водитель на месте доставки"
    case "completed":
        return "✅ Заказ выполнен. Спасибо за поездку!"
    default:
        return ""
    }
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

func cancelOrderByClient(token, clientID string) {
    collection := db.Collection("orders")
    var order models.Order
    err := collection.FindOne(context.Background(), bson.M{"client_id": clientID, "status": "pending"}).Decode(&order)
    if err != nil {
        utils.SendMessage(token, clientID, "❌ Нет активных заказов для отмены")
        return
    }

    update := bson.M{"$set": bson.M{"status": "cancelled", "cancelled_by": "client", "cancelled_at": time.Now()}}
    collection.UpdateOne(context.Background(), bson.M{"_id": order.ID}, update)

    utils.SendMessage(token, clientID, "❌ Заказ отменён")
}
