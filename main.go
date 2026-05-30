package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "os"
    "os/signal"
    "strconv"
    "strings"
    "syscall"
    "time"

    "github.com/eclipse/paho.mqtt.golang"
    "github.com/go-telegram-bot-api/telegram-bot-api/v5"
    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"
)

// Структуры
type User struct {
    ID           string    `bson:"_id,omitempty"`
    TelegramID   string    `bson:"telegram_id"`
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

var db *mongo.Database
var mqttClient mqtt.Client
//
func initMQTT() {
    broker := os.Getenv("MQTT_BROKER")
    if broker == "" {
        broker = "tcp://127.0.0.1:1883"
    }
    clientID := os.Getenv("MQTT_CLIENT_ID")
    if clientID == "" {
        clientID = "2mov_telegram"
    }
    
    opts := mqtt.NewClientOptions()  // ← эта строка должна быть
    opts.AddBroker(broker)
    opts.SetClientID(clientID)
    opts.SetCleanSession(true)
    opts.SetAutoReconnect(true)
    
    mqttClient = mqtt.NewClient(opts)
    if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
        log.Printf("⚠️ MQTT connect error: %v", token.Error())
        return
    }
    log.Println("✅ MQTT connected")
}
//

func main() {
    token := os.Getenv("TG_BOT_TOKEN")
    if token == "" {
        log.Fatal("TG_BOT_TOKEN not set")
    }

    webhookURL := os.Getenv("WEBHOOK_URL")
    if webhookURL == "" {
        log.Fatal("WEBHOOK_URL not set")
    }

    mongoURI := os.Getenv("MONGO_URI")
    if mongoURI == "" {
        mongoURI = "mongodb://localhost:27017"
    }

    // Подключение к MongoDB
    mongoClient, err := mongo.Connect(context.Background(), options.Client().ApplyURI(mongoURI))
    if err != nil {
        log.Fatal("MongoDB connection error:", err)
    }
    db = mongoClient.Database("2mov")
    log.Println("✅ Connected to MongoDB")
    
    // Инициализация MQTT
    initMQTT()

    // Создаём бота
    bot, err := tgbotapi.NewBotAPI(token)
    if err != nil {
        log.Fatal("Bot creation error:", err)
    }
    bot.Debug = true
    log.Printf("✅ Bot authorized: @%s", bot.Self.UserName)

    // Устанавливаем вебхук
    webhook, err := tgbotapi.NewWebhook(webhookURL)
    if err != nil {
        log.Fatal("Webhook creation error:", err)
    }
    _, err = bot.Request(webhook)
    if err != nil {
        log.Fatal("Webhook setup error:", err)
    }
    log.Printf("✅ Webhook set: %s", webhookURL)

    // HTTP сервер для вебхуков
    http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
        var update tgbotapi.Update
        if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
            log.Printf("Webhook decode error: %v", err)
            w.WriteHeader(http.StatusBadRequest)
            return
        }
        go handleUpdate(bot, &update)
        w.WriteHeader(http.StatusOK)
    })

    go func() {
        log.Println("🚀 Webhook server starting on :8080")
        if err := http.ListenAndServe(":8080", nil); err != nil {
            log.Fatalf("Server error: %v", err)
        }
    }()
    
    // Подписка на MQTT топики
    if mqttClient != nil && mqttClient.IsConnected() {
        // Подписка на чат-сообщения
        mqttClient.Subscribe("chat/+", 1, func(c mqtt.Client, m mqtt.Message) {
            parts := strings.Split(m.Topic(), "/")
            if len(parts) == 2 {
                userID := parts[1]
                tgChatID, err := strconv.ParseInt(userID, 10, 64)
                if err == nil {
                    sendMessage(bot, tgChatID, string(m.Payload()))
                }
            }
        })
        
        // Подписка на статусы
        mqttClient.Subscribe("status/+", 1, func(c mqtt.Client, m mqtt.Message) {
            parts := strings.Split(m.Topic(), "/")
            if len(parts) == 2 {
                userID := parts[1]
                tgChatID, err := strconv.ParseInt(userID, 10, 64)
                if err == nil {
                    status := string(m.Payload())
                    text := getStatusText(status)
                    if text != "" {
                        sendMessage(bot, tgChatID, text)
                    }
                }
            }
        })
        
        log.Println("✅ MQTT subscriptions added")
    }

    // Ожидание сигнала завершения
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    log.Println("👋 Bot stopped")
}

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

func handleUpdate(bot *tgbotapi.BotAPI, update *tgbotapi.Update) {
    if update.Message != nil {
        handleMessage(bot, update.Message)
    }
    if update.CallbackQuery != nil {
        handleCallback(bot, update.CallbackQuery)
    }
}

func handleMessage(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
    if msg == nil || msg.From == nil {
        return
    }
    startTime := time.Now()
    userID := fmt.Sprintf("%d", msg.From.ID)
    text := msg.Text
    
    log.Printf("📩 [%d] Входящее сообщение: %s", msg.From.ID, text)
    log.Printf("⏱️ [%d] Начало findOrCreateUser", msg.From.ID)
    
    user := findOrCreateUser(userID, msg.From.FirstName, msg.From.LastName, msg.From.UserName)
    
    log.Printf("⏱️ [%d] findOrCreateUser завершено за %v", msg.From.ID, time.Since(startTime))

    switch text {
    case "/start":
        reply := fmt.Sprintf("🚕 Добро пожаловать в 2MOV, %s!\nВаш рейтинг: %.1f\nОтправьте /help", msg.From.FirstName, user.Rating)
        log.Printf("⏱️ [%d] Вызов sendMessage для /start", msg.From.ID)
        sendMessage(bot, msg.Chat.ID, reply)
        log.Printf("⏱️ [%d] Команда /start обработана за %v", msg.From.ID, time.Since(startTime))
    case "/help":
        reply := "📋 Доступные команды:\n/start — начало\n/help — справка\n/profile — мой профиль\n/order — создать заказ"
        sendMessage(bot, msg.Chat.ID, reply)
    case "/profile":
        reply := fmt.Sprintf("👤 %s %s\n⭐ Рейтинг: %.1f\n🚕 Поездок: %d", user.FirstName, user.LastName, user.Rating, user.TripsCount)
        sendMessage(bot, msg.Chat.ID, reply)
    case "/order":
        session := Session{UserID: userID, Step: "from", UpdatedAt: time.Now()}
        saveSession(session)
        sendMessage(bot, msg.Chat.ID, "📍 Отправьте точку отправления (адрес или геолокацию)")
    default:
        session, err := getSession(userID)
        if err == nil {
            handleOrderCreation(bot, msg, &session)
        } else {
            sendMessage(bot, msg.Chat.ID, "❓ Неизвестная команда. Отправьте /help")
        }
    }
}

func handleCallback(bot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery) {
    if query == nil || query.Data == "" {
        return
    }
    userID := fmt.Sprintf("%d", query.From.ID)
    data := query.Data

    if strings.HasPrefix(data, "confirm_") {
        session, err := getSession(userID)
        if err == nil {
            order := Order{
                ClientID:    userID,
                FromAddress: session.FromAddress,
                FromLat:     session.FromLat,
                FromLon:     session.FromLon,
                ToAddress:   session.ToAddress,
                ToLat:       session.ToLat,
                ToLon:       session.ToLon,
                Status:      "pending",
                CreatedAt:   time.Now(),
                Price:       200,
            }
            saveOrder(order)
            deleteSession(userID)
            sendMessage(bot, query.Message.Chat.ID, "✅ Заказ создан! Водитель будет найден.")
        }
    } else if strings.HasPrefix(data, "cancel_") {
        deleteSession(userID)
        sendMessage(bot, query.Message.Chat.ID, "❌ Заказ отменён")
    }

    callback := tgbotapi.NewCallback(query.ID, "✅")
    bot.Request(callback)
}

func handleOrderCreation(bot *tgbotapi.BotAPI, msg *tgbotapi.Message, session *Session) {
    switch session.Step {
    case "from":
        if msg.Location != nil {
            session.FromLat = msg.Location.Latitude
            session.FromLon = msg.Location.Longitude
            session.FromAddress = fmt.Sprintf("%f,%f", msg.Location.Latitude, msg.Location.Longitude)
        } else if msg.Text != "" {
            session.FromAddress = msg.Text
        } else {
            sendMessage(bot, msg.Chat.ID, "❌ Отправьте адрес или геолокацию")
            return
        }
        session.Step = "to"
        saveSession(*session)
        sendMessage(bot, msg.Chat.ID, "📍 Отправьте точку назначения (адрес или геолокацию)")
    case "to":
        if msg.Location != nil {
            session.ToLat = msg.Location.Latitude
            session.ToLon = msg.Location.Longitude
            session.ToAddress = fmt.Sprintf("%f,%f", msg.Location.Latitude, msg.Location.Longitude)
        } else if msg.Text != "" {
            session.ToAddress = msg.Text
        } else {
            sendMessage(bot, msg.Chat.ID, "❌ Отправьте адрес или геолокацию")
            return
        }
        session.Step = "confirm"
        saveSession(*session)
        reply := fmt.Sprintf("🚚 Заказ:\n📍 Откуда: %s\n📍 Куда: %s\n💰 Цена: 200 ₽\n\nПодтверждаете?", session.FromAddress, session.ToAddress)
        buttons := tgbotapi.NewInlineKeyboardMarkup(
            tgbotapi.NewInlineKeyboardRow(
                tgbotapi.NewInlineKeyboardButtonData("✅ Да", "confirm_"+session.UserID),
                tgbotapi.NewInlineKeyboardButtonData("✏️ Отмена", "cancel_"+session.UserID),
            ),
        )
        newMsg := tgbotapi.NewMessage(msg.Chat.ID, reply)
        newMsg.ReplyMarkup = buttons
        bot.Send(newMsg)
    default:
        sendMessage(bot, msg.Chat.ID, "❓ Отправьте /order для нового заказа")
    }
}

func findOrCreateUser(telegramID, firstName, lastName, username string) *User {
    startTime := time.Now()
    collection := db.Collection("users")
    ctx := context.Background()
    var user User
    err := collection.FindOne(ctx, bson.M{"telegram_id": telegramID}).Decode(&user)
    
    log.Printf("⏱️ findOrCreateUser: FindOne занял %v", time.Since(startTime))
    
    if err == nil {
        collection.UpdateOne(ctx, bson.M{"telegram_id": telegramID}, bson.M{"$set": bson.M{"last_active_at": time.Now()}})
        log.Printf("⏱️ findOrCreateUser: всего (существующий) %v", time.Since(startTime))
        return &user
    }
    newUser := User{
        TelegramID:   telegramID,
        FirstName:    firstName,
        LastName:     lastName,
        Username:     username,
        Role:         "client",
        Rating:       5.0,
        TripsCount:   0,
        CreatedAt:    time.Now(),
        LastActiveAt: time.Now(),
    }
    collection.InsertOne(ctx, newUser)
    log.Printf("⏱️ findOrCreateUser: всего (новый) %v", time.Since(startTime))
    return &newUser
}

func saveSession(session Session) {
    collection := db.Collection("sessions")
    opts := options.Update().SetUpsert(true)
    filter := bson.M{"user_id": session.UserID}
    update := bson.M{"$set": session}
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
    order.ID = fmt.Sprintf("%d", time.Now().UnixNano())
    order.CreatedAt = time.Now()
    order.Price = 200.0
    _, err := collection.InsertOne(context.Background(), order)
    return err
}

func sendMessage(bot *tgbotapi.BotAPI, chatID int64, text string) {
    start := time.Now()
    msg := tgbotapi.NewMessage(chatID, text)
    _, err := bot.Send(msg)
    log.Printf("⏱️ sendMessage to %d занял %v (error: %v)", chatID, time.Since(start), err)
}
