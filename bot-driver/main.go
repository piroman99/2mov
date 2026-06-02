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

    mqttlib "github.com/eclipse/paho.mqtt.golang"
    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"
    "2mov-bot-driver/internal/handlers"
    "2mov-bot-driver/internal/models"
    "2mov-bot-driver/internal/mymqtt"
    "2mov-bot-driver/internal/utils"
)

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

    // Инициализация MQTT
    mymqtt.Init()

    // Подписка на входящие сообщения от клиентов
    if mymqtt.IsConnected() {
        mymqtt.Client.Subscribe("chat/+", 1, func(client mqttlib.Client, msg mqttlib.Message) {
            parts := strings.Split(msg.Topic(), "/")
            if len(parts) == 2 {
                driverID := parts[1]
                log.Printf("📡 MQTT received for driver %s: %s", driverID, msg.Payload())
                utils.SendMessage(token, driverID, string(msg.Payload()))
            }
        })
        log.Println("✅ MQTT driver subscribed to chat/+")
    }


    // Установка подсказок команд
    go handlers.SetCommands(token)


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
                var order models.Order
                db.Collection("orders").FindOne(context.Background(),
                    bson.M{"_id": orderID}).Decode(&order)
                if order.ClientID != "" {
                    utils.SendMessage(token, userID, fmt.Sprintf("✅ Выбран заказ #%s. Теперь отправьте сообщение.", orderID[:8]))
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
                handlers.HandleAccept(token, userID, orderID, db)
            case strings.HasPrefix(payload, "cancel_"):
                orderID := strings.TrimPrefix(payload, "cancel_")
                handlers.HandleCancel(token, userID, orderID, db)
            case strings.HasPrefix(payload, "pickup_"):
                orderID := strings.TrimPrefix(payload, "pickup_")
                handlers.HandleOrderStatus(token, orderID, "at_pickup", "📍 Водитель на месте забора", db)
            case strings.HasPrefix(payload, "depart_"):
                orderID := strings.TrimPrefix(payload, "depart_")
                handlers.HandleOrderStatus(token, orderID, "to_delivery", "🚚 Водитель выехал на доставку", db)
            case strings.HasPrefix(payload, "deliver_"):
                orderID := strings.TrimPrefix(payload, "deliver_")
                handlers.HandleOrderStatus(token, orderID, "at_delivery", "📍 Водитель на месте доставки", db)
            case strings.HasPrefix(payload, "complete_"):
                orderID := strings.TrimPrefix(payload, "complete_")
                handlers.HandleComplete(token, userID, orderID, db)
            case strings.HasPrefix(payload, "route_"):
                orderID := strings.TrimPrefix(payload, "route_")
                handlers.HandleRoute(token, userID, orderID, db)
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


    // Обработка текстового сообщения (чат)
    if text != "" && !strings.HasPrefix(text, "/") {
        if handlers.HandleChatMessage(token, userIDStr, text, db) {
            w.WriteHeader(http.StatusOK)
            return
        }
    }


        // Обычные команды
        parts := strings.Split(text, " ")
        command := parts[0]

        switch command {
        case "/start":
            utils.SendMessage(token, userIDStr, "🚕 Водительский бот 2MOV готов!\n/help — список команд")
        case "/help":
            utils.SendMessage(token, userIDStr, "📋 Команды:\n/start — приветствие\n/help — справка\n/orders — список заказов\n/myorders — мои заказы")
        case "/orders":
            handlers.HandleOrders(token, userIDStr, db)
        case "/myorders":
            handlers.HandleMyOrders(token, userIDStr, db)
        default:
            if msg, ok := update["message"].(map[string]interface{}); ok {
                if body, ok := msg["body"].(map[string]interface{}); ok {
                    if attachments, ok := body["attachments"].([]interface{}); ok {
                        for _, att := range attachments {
                            if attMap, ok := att.(map[string]interface{}); ok {
                                if attMap["type"] == "location" {
                                    lat := attMap["latitude"].(float64)
                                    lon := attMap["longitude"].(float64)
                                    handlers.HandleLocation(token, userIDStr, lat, lon)
                                }
                                if attMap["type"] == "contact" {
                                    firstName, _ := attMap["first_name"].(string)
                                    lastName, _ := attMap["last_name"].(string)
                                    phone, _ := attMap["phone_number"].(string)
                                    handlers.HandleContact(token, userIDStr, firstName, lastName, phone)
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

