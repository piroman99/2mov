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

// HandleChatMessage обрабатывает текстовые сообщения от водителя
func HandleChatMessage(token, userIDStr, text string, db *mongo.Database) (handled bool) {
    // Находим все активные заказы водителя
    var activeOrders []models.Order
    cursor, err := db.Collection("orders").Find(context.Background(),
        bson.M{
            "driver_id": userIDStr,
            "status":    bson.M{"$in": []string{"accepted", "at_pickup", "to_delivery", "at_delivery"}},
        })
    if err != nil {
        utils.SendMessage(token, userIDStr, "❌ Ошибка получения заказов")
        return true
    }
    cursor.All(context.Background(), &activeOrders)

    if len(activeOrders) == 0 {
        utils.SendMessage(token, userIDStr, "❌ Нет активных заказов")
        return true
    }

    var recipient string
    var orderID string

    if len(activeOrders) == 1 {
        orderID = activeOrders[0].ID
        recipient = activeOrders[0].ClientID
    } else {
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
        utils.SendMessageWithButtons(token, userIDStr, "Выберите заказ для отправки сообщения:", buttons)
        return true
    }

    fullText := fmt.Sprintf("🚕 Водитель: %s", text)

    msg := bson.M{
        "order_id":   orderID,
        "from_user":  "driver_" + userIDStr,
        "to_user":    "client_" + recipient,
        "text":       fullText,
        "status":     "pending",
        "created_at": time.Now(),
    }
    db.Collection("chat_messages").InsertOne(context.Background(), msg)

    if mymqtt.IsConnected() {
        topic := "chat/" + recipient
        mymqtt.Publish(topic, 1, false, fullText)
        log.Printf("📡 MQTT publish to %s: %s", topic, fullText)
    }

    utils.SendMessage(token, userIDStr, "✅ Сообщение отправлено")
    return true
}
