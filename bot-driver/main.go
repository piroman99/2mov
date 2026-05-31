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

    "github.com/eclipse/paho.mqtt.golang"
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
    CancelledBy string    `bson:"cancelled_by,omitempty"`
    CancelledAt time.Time `bson:"cancelled_at,omitempty"`
}

var db *mongo.Database
var mqttClient mqtt.Client

func initMQTT() {
    broker := os.Getenv("MQTT_BROKER")
    if broker == "" {
        broker = "tcp://62.181.53.145:1883"
    }
    opts := mqtt.NewClientOptions()
    opts.AddBroker(broker)
    opts.SetClientID("2mov_driver")
    opts.SetCleanSession(true)
    opts.SetAutoReconnect(true)

    mqttClient = mqtt.NewClient(opts)
    if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
        log.Printf("⚠️ MQTT connect error: %v", token.Error())
        return
    }
    log.Println("✅ MQTT connected")
}

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

    initMQTT()

    // Подписка на входящие сообщения от клиентов
    if mqttClient != nil && mqttClient.IsConnected() {
        mqttClient.Subscribe("chat/+", 1, func(c mqtt.Client, m mqtt.Message) {
            parts := strings.Split(m.Topic(), "/")
            if len(parts) == 2 {
                driverID := parts[1]
                log.Printf("📡 MQTT received for driver %s: %s", driverID, m.Payload())
                sendMessage(token, driverID, string(m.Payload()))
            }
        })
        log.Println("✅ MQTT driver subscribed to chat/+")
    }

    // Установка подсказок команд
    go func() {
        commands := `{"commands":[
            {"name":"start","description":"Регистрация и приветствие"},
            {"name":"help","description":"Справка по командам"},
            {"name":"orders","description":"Список доступных заказов"},
            {"name":"myorders","description":"Мои активные заказы"}
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

            // Обработка выбора заказа для отправки сообщения
            if strings.HasPrefix(payload, "send_") {
                orderID := strings.TrimPrefix(payload, "send_")
                var order Order
                db.Collection("orders").FindOne(context.Background(),
                    bson.M{"_id": orderID}).Decode(&order)
                if order.ClientID != "" {
                    sendMessage(token, userID, fmt.Sprintf("✅ Выбран заказ #%s. Теперь отправьте сообщение.", orderID[:8]))
                }
                answerURL := fmt.Sprintf("https://platform-api.max.ru/answers?callback_id=%s", callbackID)
                answerBody := map[string]interface{}{"notification": "✅"}
                answerJSON, _ := json.Marshal(answerBody)
                req, _ := http.NewRequest("POST", answerURL, bytes.NewBuffer(answerJSON))
                req.Header.Set("Authorization", token)
                req.Header.Set("Content-Type", "application/json")
                http.DefaultClient.Do(req)
                w.WriteHeader(http.StatusOK)
                return
            }

            switch {
            case strings.HasPrefix(payload, "accept_"):
                orderID := strings.TrimPrefix(payload, "accept_")
                acceptOrder(token, userID, orderID)
            case strings.HasPrefix(payload, "cancel_"):
                orderID := strings.TrimPrefix(payload, "cancel_")
                cancelOrderByDriver(token, userID, orderID)
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

        // Обработка текстового сообщения (чат) — с выбором активного заказа
        if text != "" && !strings.HasPrefix(text, "/") {
            // Находим все активные заказы водителя
            var activeOrders []Order
            cursor, err := db.Collection("orders").Find(context.Background(),
                bson.M{
                    "driver_id": userIDStr,
                    "status":    bson.M{"$in": []string{"accepted", "at_pickup", "to_delivery", "at_delivery"}},
                })
            if err != nil {
                sendMessage(token, userIDStr, "❌ Ошибка получения заказов")
                w.WriteHeader(http.StatusOK)
                return
            }
            cursor.All(context.Background(), &activeOrders)

            if len(activeOrders) == 0 {
                sendMessage(token, userIDStr, "❌ Нет активных заказов")
                w.WriteHeader(http.StatusOK)
                return
            }

            var recipient string
            var orderID string

            if len(activeOrders) == 1 {
                // Один заказ — берём его
                orderID = activeOrders[0].ID
                recipient = activeOrders[0].ClientID
            } else {
                // Несколько заказов — показываем кнопки выбора
                var buttons [][]map[string]interface{}
                for _, order := range activeOrders {
                    btnText := fmt.Sprintf("Заказ #%s: %s → %s", order.ID[:8], order.FromAddress, order.ToAddress)
                    buttons = append(buttons, []map[string]interface{}{
                        {
                            "type":    "callback",
                            "text":    btnText,
                            "payload": fmt.Sprintf("send_%s", order.ID),
                        },
                    })
                }
                sendMessageWithButtons(token, userIDStr, "Выберите заказ для отправки сообщения:", buttons)
                w.WriteHeader(http.StatusOK)
                return
            }

            // Отправляем сообщение
            fullText := fmt.Sprintf("🚕 Водитель: %s", text)

            // Сохраняем в MongoDB (для истории)
            msg := bson.M{
                "order_id":   orderID,
                "from_user":  "driver_" + userIDStr,
                "to_user":    "client_" + recipient,
                "text":       fullText,
                "status":     "pending",
                "created_at": time.Now(),
            }
            db.Collection("chat_messages").InsertOne(context.Background(), msg)

            // Отправляем через MQTT
            if mqttClient != nil && mqttClient.IsConnected() {
                topic := "chat/" + recipient
                mqttClient.Publish(topic, 1, false, fullText)
                log.Printf("📡 MQTT publish to %s: %s", topic, fullText)
            }

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
            sendMessage(token, userIDStr, "📋 Команды:\n/start — приветствие\n/orders — список заказов\n/myorders — мои заказы")
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
        case "/myorders":
            collection := db.Collection("orders")
            filter := bson.M{"driver_id": userIDStr, "$or": []bson.M{
                {"status": "accepted"},
                {"status": "at_pickup"},
                {"status": "to_delivery"},
                {"status": "at_delivery"},
            }}
            cursor, err := collection.Find(context.Background(), filter)
            if err != nil {
                sendMessage(token, userIDStr, "❌ Ошибка получения заказов")
                break
            }
            var orders []Order
            cursor.All(context.Background(), &orders)
            if len(orders) == 0 {
                sendMessage(token, userIDStr, "📭 Нет принятых заказов")
                break
            }
            for _, o := range orders {
                moscowTime := o.CreatedAt.Add(3 * time.Hour)
                timeStr := moscowTime.Format("02.01 15:04")

                reply := fmt.Sprintf("✅ Заказ #%s\n📅 %s\n📍 %s → %s\n💰 %.0f ₽\n📌 Статус: %s",
                    o.ID[:8],
                    timeStr,
                    o.FromAddress,
                    o.ToAddress,
                    o.Price,
                    o.Status,
                )

                var buttons [][]map[string]interface{}
                switch o.Status {
                case "accepted":
                    buttons = [][]map[string]interface{}{
                        {
                            {"type": "callback", "text": "📍 Прибыл на забор", "payload": fmt.Sprintf("pickup_%s", o.ID)},
                            {"type": "callback", "text": "❌ Отменить заказ", "payload": fmt.Sprintf("cancel_%s", o.ID)},
                        },
                    }
                case "at_pickup":
                    buttons = [][]map[string]interface{}{
                        {
                            {"type": "callback", "text": "🚚 Выехал на доставку", "payload": fmt.Sprintf("depart_%s", o.ID)},
                            {"type": "callback", "text": "❌ Отменить заказ", "payload": fmt.Sprintf("cancel_%s", o.ID)},
                        },
                    }
                case "to_delivery":
                    buttons = [][]map[string]interface{}{
                        {
                            {"type": "callback", "text": "📍 Прибыл на доставку", "payload": fmt.Sprintf("deliver_%s", o.ID)},
                        },
                    }
                case "at_delivery":
                    buttons = [][]map[string]interface{}{
                        {
                            {"type": "callback", "text": "✅ Завершить", "payload": fmt.Sprintf("complete_%s", o.ID)},
                        },
                    }
                }
                // Добавляем маршрут
                buttons = append(buttons, []map[string]interface{}{
                    {"type": "callback", "text": "🗺️ Маршрут", "payload": fmt.Sprintf("route_%s", o.ID)},
                })
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

    log.Println("✅ Водительский бот запущен")
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
}

// ----------------------------------------------------------------
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ (копируем из вашего старого файла)
// ----------------------------------------------------------------

func acceptOrder(token, driverID, orderIDHex string) {
    collection := db.Collection("orders")
    result, err := collection.UpdateOne(context.Background(),
        bson.M{"_id": orderIDHex, "status": "pending"},
        bson.M{"$set": bson.M{"status": "accepted", "driver_id": driverID}})
    if err != nil || result.MatchedCount == 0 {
        sendMessage(token, driverID, "❌ Не удалось принять заказ. Возможно, его уже взяли.")
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

    // MQTT уведомление
    if mqttClient != nil && mqttClient.IsConnected() {
        topic := fmt.Sprintf("status/%s", order.ClientID)
        mqttClient.Publish(topic, 1, false, "accepted")
        log.Printf("📡 MQTT publish status to %s: accepted", topic)
    }

    // Отправляем водителю статус
    sendOrderStatusToDriver(token, order)
}

func cancelOrderByDriver(token, driverID, orderIDHex string) {
    collection := db.Collection("orders")
    var order Order
    err := collection.FindOne(context.Background(), bson.M{"_id": orderIDHex, "driver_id": driverID}).Decode(&order)
    if err != nil {
        sendMessage(token, driverID, "❌ Заказ не найден")
        return
    }

    allowedStatuses := []string{"accepted", "at_pickup"}
    allowed := false
    for _, s := range allowedStatuses {
        if order.Status == s {
            allowed = true
            break
        }
    }
    if !allowed {
        sendMessage(token, driverID, "❌ Отмена невозможна. Заказ уже в пути или доставке.")
        return
    }

    // Возвращаем заказ в статус pending
    update := bson.M{"$set": bson.M{
        "status":     "pending",
        "driver_id":  nil,
        "updated_at": time.Now(),
    }}
    collection.UpdateOne(context.Background(), bson.M{"_id": orderIDHex}, update)

    // Уведомление клиенту
    clientMsg := bson.M{
        "order_id":   orderIDHex,
        "from_user":  "system",
        "to_user":    "client_" + order.ClientID,
        "text":       "❌ Водитель отменил заказ. Заказ снова доступен для других водителей.",
        "status":     "pending",
        "created_at": time.Now(),
    }
    db.Collection("chat_messages").InsertOne(context.Background(), clientMsg)

    // MQTT уведомление
    if mqttClient != nil && mqttClient.IsConnected() {
        topic := fmt.Sprintf("status/%s", order.ClientID)
        mqttClient.Publish(topic, 1, false, "pending")
        log.Printf("📡 MQTT publish status to %s: pending", topic)
    }

    // Закрываем чат
    db.Collection("chats").UpdateOne(context.Background(),
        bson.M{"order_id": orderIDHex},
        bson.M{"$set": bson.M{"status": "closed", "updated_at": time.Now()}})

    sendMessage(token, driverID, "✅ Заказ отменён и возвращён в общий список")
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

    // MQTT уведомление
    if mqttClient != nil && mqttClient.IsConnected() {
        topic := fmt.Sprintf("status/%s", order.ClientID)
        mqttClient.Publish(topic, 1, false, status)
        log.Printf("📡 MQTT publish status to %s: %s", topic, status)
    }

    // Отправляем водителю обновлённый статус
    sendOrderStatusToDriver(token, order)

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

    // Сообщение клиенту
    clientMsg := bson.M{
        "order_id":   orderIDHex,
        "from_user":  "system",
        "to_user":    "client_" + order.ClientID,
        "text":       "✅ Заказ выполнен. Спасибо за поездку!",
        "status":     "pending",
        "created_at": time.Now(),
    }
    db.Collection("chat_messages").InsertOne(context.Background(), clientMsg)

    // MQTT уведомление
    if mqttClient != nil && mqttClient.IsConnected() {
        topic := fmt.Sprintf("status/%s", order.ClientID)
        mqttClient.Publish(topic, 1, false, "completed")
        log.Printf("📡 MQTT publish status to %s: completed", topic)
    }

    // Закрываем чат через 15 секунд
    go func() {
        time.Sleep(15 * time.Second)
        db.Collection("chats").UpdateOne(context.Background(),
            bson.M{"order_id": orderIDHex},
            bson.M{"$set": bson.M{"status": "closed", "updated_at": time.Now()}})
        log.Printf("✅ Чат для заказа %s закрыт", orderIDHex[:8])
    }()

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
            {
                {"type": "callback", "text": "📍 Прибыл на забор", "payload": fmt.Sprintf("pickup_%s", order.ID)},
                {"type": "callback", "text": "❌ Отменить заказ", "payload": fmt.Sprintf("cancel_%s", order.ID)},
            },
        }
    case "at_pickup":
        buttons = [][]map[string]interface{}{
            {
                {"type": "callback", "text": "🚚 Выехал на доставку", "payload": fmt.Sprintf("depart_%s", order.ID)},
                {"type": "callback", "text": "❌ Отменить заказ", "payload": fmt.Sprintf("cancel_%s", order.ID)},
            },
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
