package handlers

import (
    "log"
    "strconv"
    "strings"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
    "2mov/internal/mymqtt"
    "2mov/internal/utils"
)

func SubscribeToStatus(bot *tgbotapi.BotAPI) {
    if !mymqtt.IsConnected() {
        return
    }
    mymqtt.Client.Subscribe("status/+", 1, func(c mqtt.Client, msg mqtt.Message) {
        parts := strings.Split(msg.Topic(), "/")
        if len(parts) == 2 {
            userID := parts[1]
            tgChatID, err := strconv.ParseInt(userID, 10, 64)
            if err != nil {
                return
            }
            status := string(msg.Payload())
            text := getStatusText(status)
            if text != "" {
                utils.SendMessage(bot, tgChatID, text)
            }
        }
    })
    log.Println("✅ MQTT subscribed to status/+")
}

func SubscribeToChat(bot *tgbotapi.BotAPI) {
    if !mymqtt.IsConnected() {
        return
    }
    mymqtt.Client.Subscribe("chat/+", 1, func(c mqtt.Client, msg mqtt.Message) {
        parts := strings.Split(msg.Topic(), "/")
        if len(parts) == 2 {
            userID := parts[1]
            tgChatID, err := strconv.ParseInt(userID, 10, 64)
            if err != nil {
                return
            }
            utils.SendMessage(bot, tgChatID, string(msg.Payload()))
        }
    })
    log.Println("✅ MQTT subscribed to chat/+")
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
