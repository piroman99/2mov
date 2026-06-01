package handlers

import (
    "context"
    "fmt"
    "strconv"
    "time"

    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/mongo"
    "2mov-bot-driver/internal/models"
    "2mov-bot-driver/internal/utils"
)

func HandleOrders(token, userIDStr string, db *mongo.Database) {
    collection := db.Collection("orders")
    filter := bson.M{"status": "pending"}
    cursor, err := collection.Find(context.Background(), filter)
    if err != nil {
        utils.SendMessage(token, userIDStr, "❌ Ошибка получения заказов")
        return
    }
    defer cursor.Close(context.Background())

    var orders []models.Order
    if err = cursor.All(context.Background(), &orders); err != nil {
        utils.SendMessage(token, userIDStr, "❌ Ошибка чтения заказов")
        return
    }
    if len(orders) == 0 {
        utils.SendMessage(token, userIDStr, "📭 Нет активных заказов")
        return
    }

    usersCollection := db.Collection("users")
    for _, o := range orders {
        var client struct {
            FirstName  string  `bson:"first_name"`
            LastName   string  `bson:"last_name"`
            Rating     float64 `bson:"rating"`
            TripsCount int     `bson:"trips_count"`
        }
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
            o.ID[:8], timeStr, clientName, client.Rating, client.TripsCount, o.FromAddress, o.ToAddress, o.Price)
        buttons := [][]map[string]interface{}{
            {
                {"type": "callback", "text": "✅ Принять", "payload": fmt.Sprintf("accept_%s", o.ID)},
                {"type": "callback", "text": "🗺️ Маршрут", "payload": fmt.Sprintf("route_%s", o.ID)},
            },
        }
        utils.SendMessageWithButtons(token, userIDStr, reply, buttons)
    }
}

func HandleMyOrders(token, userIDStr string, db *mongo.Database) {
    collection := db.Collection("orders")
    filter := bson.M{
        "driver_id": userIDStr,
        "$or": []bson.M{
            {"status": "accepted"},
            {"status": "at_pickup"},
            {"status": "to_delivery"},
            {"status": "at_delivery"},
        },
    }
    cursor, err := collection.Find(context.Background(), filter)
    if err != nil {
        utils.SendMessage(token, userIDStr, "❌ Ошибка получения заказов")
        return
    }
    defer cursor.Close(context.Background())

    var orders []models.Order
    if err = cursor.All(context.Background(), &orders); err != nil {
        utils.SendMessage(token, userIDStr, "❌ Ошибка чтения заказов")
        return
    }
    if len(orders) == 0 {
        utils.SendMessage(token, userIDStr, "📭 Нет принятых заказов")
        return
    }

    for _, o := range orders {
        moscowTime := o.CreatedAt.Add(3 * time.Hour)
        timeStr := moscowTime.Format("02.01 15:04")

        reply := fmt.Sprintf("✅ Заказ #%s\n📅 %s\n📍 %s → %s\n💰 %.0f ₽\n📌 Статус: %s",
            o.ID[:8], timeStr, o.FromAddress, o.ToAddress, o.Price, o.Status)

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
        utils.SendMessageWithButtons(token, userIDStr, reply, buttons)
    }
}
