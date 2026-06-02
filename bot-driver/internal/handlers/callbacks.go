package handlers

import (
    "context"
    "fmt"
    "log"
    "time"

    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/mongo"
    "2mov-bot-driver/internal/models"
    "2mov-bot-driver/internal/mymqtt"
    "2mov-bot-driver/internal/utils"
)

func HandleAccept(token, driverID, orderIDHex string, db *mongo.Database) {
    collection := db.Collection("orders")
    result, err := collection.UpdateOne(context.Background(),
        bson.M{"_id": orderIDHex, "status": "pending"},
        bson.M{"$set": bson.M{"status": "accepted", "driver_id": driverID}})
    if err != nil || result.MatchedCount == 0 {
        utils.SendMessage(token, driverID, "❌ Не удалось принять заказ. Возможно, его уже взяли.")
        return
    }

    var order models.Order
    collection.FindOne(context.Background(), bson.M{"_id": orderIDHex}).Decode(&order)

    chat := bson.M{
        "order_id":   orderIDHex,
        "driver_id":  driverID,
        "client_id":  order.ClientID,
        "status":     "active",
        "created_at": time.Now(),
        "updated_at": time.Now(),
    }
    db.Collection("chats").InsertOne(context.Background(), chat)

    clientMsg := bson.M{
        "order_id":   orderIDHex,
        "from_user":  "system",
        "to_user":    "client_" + order.ClientID,
        "text":       "✅ Водитель принял ваш заказ!",
        "status":     "pending",
        "created_at": time.Now(),
    }
    db.Collection("chat_messages").InsertOne(context.Background(), clientMsg)

    if mymqtt.IsConnected() {
        topic := fmt.Sprintf("status/%s", order.ClientID)
        mymqtt.Publish(topic, 1, false, "accepted")
        log.Printf("📡 MQTT publish status to %s: accepted", topic)
    }

    HandleSendOrderStatus(token, order, db)
}

func HandleCancel(token, driverID, orderIDHex string, db *mongo.Database) {
    collection := db.Collection("orders")
    var order models.Order
    err := collection.FindOne(context.Background(), bson.M{"_id": orderIDHex, "driver_id": driverID}).Decode(&order)
    if err != nil {
        utils.SendMessage(token, driverID, "❌ Заказ не найден")
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
        utils.SendMessage(token, driverID, "❌ Отмена невозможна. Заказ уже в пути или доставке.")
        return
    }

    update := bson.M{"$set": bson.M{
        "status":     "pending",
        "driver_id":  nil,
        "updated_at": time.Now(),
    }}
    collection.UpdateOne(context.Background(), bson.M{"_id": orderIDHex}, update)

    clientMsg := bson.M{
        "order_id":   orderIDHex,
        "from_user":  "system",
        "to_user":    "client_" + order.ClientID,
        "text":       "❌ Водитель отменил заказ. Заказ снова доступен для других водителей.",
        "status":     "pending",
        "created_at": time.Now(),
    }
    db.Collection("chat_messages").InsertOne(context.Background(), clientMsg)

    if mymqtt.IsConnected() {
        topic := fmt.Sprintf("status/%s", order.ClientID)
        mymqtt.Publish(topic, 1, false, "pending")
        log.Printf("📡 MQTT publish status to %s: pending", topic)
    }

    db.Collection("chats").UpdateOne(context.Background(),
        bson.M{"order_id": orderIDHex},
        bson.M{"$set": bson.M{"status": "closed", "updated_at": time.Now()}})

    utils.SendMessage(token, driverID, "✅ Заказ отменён и возвращён в общий список")
}

func HandleOrderStatus(token, orderID, status, notificationText string, db *mongo.Database) {
    collection := db.Collection("orders")
    filter := bson.M{"_id": orderID}
    update := bson.M{"$set": bson.M{"status": status}}
    collection.UpdateOne(context.Background(), filter, update)

    var order models.Order
    collection.FindOne(context.Background(), filter).Decode(&order)

    clientMsg := bson.M{
        "order_id":   orderID,
        "from_user":  "system",
        "to_user":    "client_" + order.ClientID,
        "text":       notificationText,
        "status":     "pending",
        "created_at": time.Now(),
    }
    db.Collection("chat_messages").InsertOne(context.Background(), clientMsg)

    if mymqtt.IsConnected() {
        topic := fmt.Sprintf("status/%s", order.ClientID)
        mymqtt.Publish(topic, 1, false, status)
        log.Printf("📡 MQTT publish status to %s: %s", topic, status)
    }

    HandleSendOrderStatus(token, order, db)

    if status == "to_delivery" {
        HandleRouteToDelivery(token, order, db)
    }
}

func HandleComplete(token, driverID, orderIDHex string, db *mongo.Database) {
    collection := db.Collection("orders")
    filter := bson.M{"_id": orderIDHex, "driver_id": driverID, "status": "at_delivery"}
    update := bson.M{"$set": bson.M{"status": "completed"}}

    result, err := collection.UpdateOne(context.Background(), filter, update)
    if err != nil || result.MatchedCount == 0 {
        utils.SendMessage(token, driverID, "❌ Не удалось завершить заказ")
        return
    }

    var order models.Order
    collection.FindOne(context.Background(), bson.M{"_id": orderIDHex}).Decode(&order)

    clientMsg := bson.M{
        "order_id":   orderIDHex,
        "from_user":  "system",
        "to_user":    "client_" + order.ClientID,
        "text":       "✅ Заказ выполнен. Спасибо за поездку!",
        "status":     "pending",
        "created_at": time.Now(),
    }
    db.Collection("chat_messages").InsertOne(context.Background(), clientMsg)

    if mymqtt.IsConnected() {
        topic := fmt.Sprintf("status/%s", order.ClientID)
        mymqtt.Publish(topic, 1, false, "completed")
        log.Printf("📡 MQTT publish status to %s: completed", topic)
    }

    go func() {
        time.Sleep(15 * time.Second)
        db.Collection("chats").UpdateOne(context.Background(),
            bson.M{"order_id": orderIDHex},
            bson.M{"$set": bson.M{"status": "closed", "updated_at": time.Now()}})
        log.Printf("✅ Чат для заказа %s закрыт", orderIDHex[:8])
    }()

    utils.SendMessage(token, driverID, "✅ Заказ завершён! Спасибо за работу.")
}

func HandleRoute(token, driverID, orderIDHex string, db *mongo.Database) {
    var order models.Order
    collection := db.Collection("orders")
    err := collection.FindOne(context.Background(), bson.M{"_id": orderIDHex}).Decode(&order)
    if err != nil {
        utils.SendMessage(token, driverID, "❌ Заказ не найден")
        return
    }
    if order.FromLat != 0 && order.FromLon != 0 {
        mapLink := fmt.Sprintf("https://yandex.ru/maps/?rtext=~%f,%f", order.FromLat, order.FromLon)
        utils.SendMessage(token, driverID, fmt.Sprintf("🗺️ Маршрут до точки забора:\n%s", mapLink))
    } else {
        utils.SendMessage(token, driverID, fmt.Sprintf("📍 Адрес забора: %s\nПостройте маршрут самостоятельно", order.FromAddress))
    }
}

func HandleRouteToDelivery(token string, order models.Order, db *mongo.Database) {
    if order.ToLat != 0 && order.ToLon != 0 {
        mapLink := fmt.Sprintf("https://yandex.ru/maps/?rtext=~%f,%f", order.ToLat, order.ToLon)
        utils.SendMessage(token, order.DriverID, fmt.Sprintf("🗺️ Маршрут до точки доставки:\n%s", mapLink))
    } else {
        utils.SendMessage(token, order.DriverID, fmt.Sprintf("📍 Адрес доставки: %s\nПостройте маршрут самостоятельно", order.ToAddress))
    }
}

func HandleSendOrderStatus(token string, order models.Order, db *mongo.Database) {
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

    utils.SendMessageWithButtons(token, order.DriverID, text, buttons)
}
