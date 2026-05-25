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
    "strconv"
    "strings"
    "syscall"
    "time"

    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"
)

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

var db *mongo.Database

func main() {
    token := os.Getenv("MAX_BOT_TOKEN")
    if token == "" {
        log.Fatal("MAX_BOT_TOKEN not set")
    }

    mongoURI := os.Getenv("MONGO_URI")
    if mongoURI == "" {
        mongoURI = "mongodb://localhost:27017"
    }
    mongoClient, err := mongo.Connect(context.Background(), options.Client().ApplyURI(mongoURI))
    if err != nil {
        log.Fatal("❌ Ошибка подключения к MongoDB:", err)
    }
    db = mongoClient.Database("2mov")
    log.Println("✅ Подключение к MongoDB установлено")

    http.HandleFunc("/webhookd", func(w http.ResponseWriter, r *http.Request) {
        body, _ := io.ReadAll(r.Body)
        log.Printf("📩 %s", string(body))

        var update map[string]interface{}
        json.Unmarshal(body, &update)

        // Обработка callback-кнопок
        if cb, ok := update["callback"].(map[string]interface{}); ok {
            callbackID := cb["callback_id"].(string)
            payload := cb["payload"].(string)

            var userID string
            if userObj, ok := cb["user"].(map[string]interface{}); ok {
                if id, ok := userObj["user_id"]; ok {
                    userID = fmt.Sprintf("%.0f", id.(float64))
                }
            }

            log.Printf("🔘 Callback: user=%s, payload=%s", userID, payload)

            switch {
            case strings.HasPrefix(payload, "accept_"):
                orderID := strings.TrimPrefix(payload, "accept_")
                acceptOrder(token, userID, orderID)
            case strings.HasPrefix(payload, "pickup_"):
                orderID := strings.TrimPrefix(payload, "pickup_")
                updateOrderStatus(token, orderID, "at_pickup", "📍 Водитель на месте забора")
            case strings.HasPrefix(payload, "depart_"):
                orderID := strings.TrimPrefix(payload, "depart_")
                updateOrderStatus(token, orderID, "to_delivery", "🚚 Водитель выехал на доставку")
            case strings.HasPrefix(payload, "deliver_"):
                orderID := strings.TrimPrefix(payload, "deliver_")
                updateOrderStatus(token, orderID, "at_delivery", "📍 Водитель на месте доставки")
            case strings.HasPrefix(payload, "complete_"):
                orderID := strings.TrimPrefix(payload, "complete_")
                completeOrder(token, userID, orderID)
            case strings.HasPrefix(payload, "route_"):
                orderID := strings.TrimPrefix(payload, "route_")
                routeOrder(token, userID, orderID)
            }

            answerURL := fmt.Sprintf("https://platform-api.max.ru/answers?callback_id=%s", callbackID)
            answerBody := map[string]interface{}{
                "notification": "✅",
            }
            answerJSON, _ := json.Marshal(answerBody)
            req, _ := http.NewRequest("POST", answerURL, bytes.NewBuffer(answerJSON))
            req.Header.Set("Authorization", token)
            req.Header.Set("Content-Type", "application/json")
            http.DefaultClient.Do(req)

            w.WriteHeader(http.StatusOK)
            return
        }

        // Обычные сообщения
        var text string
        var userID int
        if msg, ok := update["message"].(map[string]interface{}); ok {
            if body, ok := msg["body"].(map[string]interface{}); ok {
                text, _ = body["text"].(string)
            }
            if sender, ok := msg["sender"].(map[string]interface{}); ok {
                if id, ok := sender["user_id"]; ok {
                    userID = int(id.(float64))
                }
            }
        }

        userIDStr := fmt.Sprintf("%d", userID)

        // Обработка чата (отправка сообщения клиенту)
        var chat struct {
            DriverID string `bson:"driver_id"`
            ClientID string `bson:"client_id"`
            Status   string `bson:"status"`
        }
        err := db.Collection("chats").FindOne(context.Background(),
            bson.M{"$or": []bson.M{
                {"driver_id": userIDStr, "status": "active"},
                {"client_id": userIDStr, "status": "active"},
            }}).Decode(&chat)

        if err == nil && text != "" && !strings.HasPrefix(text, "/") {
            log.Printf("💬 Чат: user=%s, text=%s", userIDStr, text)
            var recipient string
            var senderRole string
            if userIDStr == chat.DriverID {
                recipient = chat.ClientID
                senderRole = "🚕 Водитель"
            } else {
                recipient = chat.DriverID
                senderRole = "👤 Клиент"
            }
            msg := bson.M{
                "order_id":   "",
                "from_user":  "driver_" + userIDStr,
                "to_user":    "client_" + recipient,
                "text":       fmt.Sprintf("%s: %s", senderRole, text),
                "status":     "pending",
                "created_at": time.Now(),
            }
            db.Collection("chat_messages").InsertOne(context.Background(), msg)
            sendMessage(token, userIDStr, "✅ Сообщение отправлено")
            w.WriteHeader(http.StatusOK)
            return
        }

        // Обычные команды
        parts := strings.Split(text, " ")
        command := parts[0]

        switch command {
        case "/start":
            sendMessage(token, userIDStr, "🚕 Водительский бот 2MOV готов!\n/help — список команд")
        case "/help":
            sendMessage(token, userIDStr, "📋 Команды:\n/start — приветствие\n/orders — список заказов")
        case "/orders":
            collection := db.Collection("orders")
            filter := bson.M{"status": "pending"}
            cursor, err := collection.Find(context.Background(), filter)
            if err != nil {
                sendMessage(token, userIDStr, "❌ Ошибка получения заказов")
                break
            }
            var orders []Order
            cursor.All(context.Background(), &orders)
            if len(orders) == 0 {
                sendMessage(token, userIDStr, "📭 Нет активных заказов")
                break
            }
            for _, o := range orders {
                var client struct {
                    FirstName  string  `bson:"first_name"`
                    LastName   string  `bson:"last_name"`
                    Rating     float64 `bson:"rating"`
                    TripsCount int     `bson:"trips_count"`
                }
                usersCollection := db.Collection("users")
                clientIDint, _ := strconv.Atoi(o.ClientID)
                usersCollection.FindOne(context.Background(), bson.M{"max_user_id": clientIDint}).Decode(&client)

                clientName := fmt.Sprintf("Клиент #%s", o.ClientID)
                if client.FirstName != "" {
                    lastNameInitial := ""
                    if len(client.LastName) > 0 {
                        lastNameInitial = string([]rune(client.LastName)[0]) + "."
                    }
                    clientName = fmt.Sprintf("%s %s", client.FirstName, lastNameInitial)
                }

                moscowTime := o.CreatedAt.Add(3 * time.Hour)
                timeStr := moscowTime.Format("02.01 15:04")

                reply := fmt.Sprintf("🔹 Заказ #%s\n📅 %s\n👤 %s\n⭐ Рейтинг: %.1f\n🚕 Поездок: %d\n📍 %s → %s\n💰 %.0f ₽",
                    o.ID[:8],
                    timeStr,
                    clientName,
                    client.Rating,
                    client.TripsCount,
                    o.FromAddress,
                    o.ToAddress,
                    o.Price,
                )
                buttons := [][]map[string]interface{}{
                    {
                        {
                            "type": "callback",
                            "text": "✅ Принять",
                            "payload": fmt.Sprintf("accept_%s", o.ID),
                        },
                        {
                            "type": "callback",
                            "text": "🗺️ Маршрут",
                            "payload": fmt.Sprintf("route_%s", o.ID),
                        },
                    },
                }
                sendMessageWithButtons(token, userIDStr, reply, buttons)
            }
        default:
            // Обработка вложений
            if msg, ok := update["message"].(map[string]interface{}); ok {
                if body, ok := msg["body"].(map[string]interface{}); ok {
                    if attachments, ok := body["attachments"].([]interface{}); ok {
                        for _, att := range attachments {
                            if attMap, ok := att.(map[string]interface{}); ok {
                                if attMap["type"] == "location" {
                                    lat := attMap["latitude"].(float64)
                                    lon := attMap["longitude"].(float64)
                                    sendMessage(token, userIDStr, fmt.Sprintf("📍 https://yandex.ru/maps/?pt=%f,%f&z=15", lon, lat))
                                }
                                if attMap["type"] == "contact" {
                                    firstName, _ := attMap["first_name"].(string)
                                    lastName, _ := attMap["last_name"].(string)
                                    phone, _ := attMap["phone_number"].(string)
                                    sendMessage(token, userIDStr, fmt.Sprintf("📱 %s %s\n%s", firstName, lastName, phone))
                                }
                            }
                        }
                    }
                }
            }
        }

        w.WriteHeader(http.StatusOK)
    })

    go func() {
        log.Println("✅ HTTP-сервер запущен на :8080")
        http.ListenAndServe(":8080", nil)
    }()

    // Фоновая проверка сообщений для водителя
    go func() {
        ticker := time.NewTicker(3 * time.Second)
        for range ticker.C {
            var chats []struct {
                DriverID string `bson:"driver_id"`
            }
            cursor, err := db.Collection("chats").Find(context.Background(), bson.M{"status": "active"})
            if err != nil {
                continue
            }
            cursor.All(context.Background(), &chats)

            for _, chat := range chats {
                var messages []bson.M
                msgCursor, err := db.Collection("chat_messages").Find(context.Background(),
                    bson.M{"to_user": "driver_" + chat.DriverID, "status": "pending"})
                if err != nil {
                    continue
                }
                msgCursor.All(context.Background(), &messages)

                for _, msg := range messages {
                    text := msg["text"].(string)
                    sendMessage(token, chat.DriverID, fmt.Sprintf("💬 %s", text))
                    db.Collection("chat_messages").UpdateOne(context.Background(),
                        bson.M{"_id": msg["_id"]},
                        bson.M{"$set": bson.M{"status": "delivered", "delivered_at": time.Now()}})
                }
            }
        }
    }()

    log.Println("✅ Водительский бот запущен")
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
}

func acceptOrder(token, driverID, orderIDHex string) {
    collection := db.Collection("orders")
    filter := bson.M{"_id": orderIDHex, "status": "pending"}
    update := bson.M{"$set": bson.M{"status": "accepted", "driver_id": driverID}}

    result, err := collection.UpdateOne(context.Background(), filter, update)
    if err != nil || result.MatchedCount == 0 {
        sendMessage(token, driverID, "❌ Не удалось принять заказ")
        return
    }

    var order Order
    collection.FindOne(context.Background(), bson.M{"_id": orderIDHex}).Decode(&order)

    // Создаём чат
    chat := bson.M{
        "order_id":   orderIDHex,
        "driver_id":  driverID,
        "client_id":  order.ClientID,
        "status":     "active",
        "created_at": time.Now(),
        "updated_at": time.Now(),
    }
    db.Collection("chats").InsertOne(context.Background(), chat)

    // Уведомление клиенту
    clientMsg := bson.M{
        "order_id":   orderIDHex,
        "from_user":  "system",
        "to_user":    "client_" + order.ClientID,
        "text":       "✅ Водитель принял ваш заказ!",
        "status":     "pending",
        "created_at": time.Now(),
    }
    db.Collection("chat_messages").InsertOne(context.Background(), clientMsg)

    // Отправляем водителю статус
    sendOrderStatusToDriver(token, order)
}

func updateOrderStatus(token, orderID, status, notificationText string) {
    collection := db.Collection("orders")
    filter := bson.M{"_id": orderID}
    update := bson.M{"$set": bson.M{"status": status}}
    collection.UpdateOne(context.Background(), filter, update)

    var order Order
    collection.FindOne(context.Background(), filter).Decode(&order)

    // Уведомление клиенту
    clientMsg := bson.M{
        "order_id":   orderID,
        "from_user":  "system",
        "to_user":    "client_" + order.ClientID,
        "text":       notificationText,
        "status":     "pending",
        "created_at": time.Now(),
    }
    db.Collection("chat_messages").InsertOne(context.Background(), clientMsg)

    // Отправляем водителю обновлённый статус
    sendOrderStatusToDriver(token, order)

    // Если выехал на доставку — отправляем маршрут до доставки
    if status == "to_delivery" {
        routeToDelivery(token, order)
    }
}

func completeOrder(token, driverID, orderIDHex string) {
    collection := db.Collection("orders")
    filter := bson.M{"_id": orderIDHex, "driver_id": driverID, "status": "at_delivery"}
    update := bson.M{"$set": bson.M{"status": "completed"}}

    result, err := collection.UpdateOne(context.Background(), filter, update)
    if err != nil || result.MatchedCount == 0 {
        sendMessage(token, driverID, "❌ Не удалось завершить заказ")
        return
    }

    var order Order
    collection.FindOne(context.Background(), bson.M{"_id": orderIDHex}).Decode(&order)

    // Закрываем чат
    db.Collection("chats").UpdateOne(context.Background(),
        bson.M{"order_id": orderIDHex},
        bson.M{"$set": bson.M{"status": "closed", "updated_at": time.Now()}})

    // Уведомление клиенту
    clientMsg := bson.M{
        "order_id":   orderIDHex,
        "from_user":  "system",
        "to_user":    "client_" + order.ClientID,
        "text":       "✅ Заказ выполнен. Спасибо!",
        "status":     "pending",
        "created_at": time.Now(),
    }
    db.Collection("chat_messages").InsertOne(context.Background(), clientMsg)

    sendMessage(token, driverID, "✅ Заказ завершён! Спасибо за работу.")
}

func routeOrder(token, driverID, orderIDHex string) {
    var order Order
    collection := db.Collection("orders")
    err := collection.FindOne(context.Background(), bson.M{"_id": orderIDHex}).Decode(&order)
    if err != nil {
        sendMessage(token, driverID, "❌ Заказ не найден")
        return
    }
    if order.FromLat != 0 && order.FromLon != 0 {
        mapLink := fmt.Sprintf("https://yandex.ru/maps/?rtext=~%f,%f", order.FromLat, order.FromLon)
        sendMessage(token, driverID, fmt.Sprintf("🗺️ Маршрут до точки забора:\n%s", mapLink))
    } else {
        sendMessage(token, driverID, fmt.Sprintf("📍 Адрес забора: %s\nПостройте маршрут самостоятельно", order.FromAddress))
    }
}

func routeToDelivery(token string, order Order) {
    if order.ToLat != 0 && order.ToLon != 0 {
        mapLink := fmt.Sprintf("https://yandex.ru/maps/?rtext=~%f,%f", order.ToLat, order.ToLon)
        sendMessage(token, order.DriverID, fmt.Sprintf("🗺️ Маршрут до точки доставки:\n%s", mapLink))
    } else {
        sendMessage(token, order.DriverID, fmt.Sprintf("📍 Адрес доставки: %s\nПостройте маршрут самостоятельно", order.ToAddress))
    }
}

func sendOrderStatusToDriver(token string, order Order) {
    var buttons [][]map[string]interface{}
    switch order.Status {
    case "accepted":
        buttons = [][]map[string]interface{}{
            {{"type": "callback", "text": "📍 Прибыл на забор", "payload": fmt.Sprintf("pickup_%s", order.ID)}},
        }
    case "at_pickup":
        buttons = [][]map[string]interface{}{
            {{"type": "callback", "text": "🚚 Выехал на доставку", "payload": fmt.Sprintf("depart_%s", order.ID)}},
        }
    case "to_delivery":
        buttons = [][]map[string]interface{}{
            {{"type": "callback", "text": "📍 Прибыл на доставку", "payload": fmt.Sprintf("deliver_%s", order.ID)}},
        }
    case "at_delivery":
        buttons = [][]map[string]interface{}{
            {{"type": "callback", "text": "✅ Завершить", "payload": fmt.Sprintf("complete_%s", order.ID)}},
        }
    default:
        return
    }
    // Добавляем маршрут до забора
    buttons = append(buttons, []map[string]interface{}{
        {"type": "callback", "text": "🗺️ Маршрут до забора", "payload": fmt.Sprintf("route_%s", order.ID)},
    })

    moscowTime := order.CreatedAt.Add(3 * time.Hour)
    timeStr := moscowTime.Format("02.01 15:04")

    text := fmt.Sprintf("✅ Заказ #%s\n📅 %s\n📍 %s → %s\n💰 %.0f ₽\n📌 Статус: %s",
        order.ID[:8], timeStr, order.FromAddress, order.ToAddress, order.Price, order.Status)

    sendMessageWithButtons(token, order.DriverID, text, buttons)
}

func sendMessage(token, chatID, text string) {
    sendMaxMessage(token, chatID, text)
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

func sendMessageWithButtons(token, chatID, text string, buttons [][]map[string]interface{}) {
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
