package utils

import (
    "log"
    "time"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func SendMessage(bot *tgbotapi.BotAPI, chatID int64, text string) {
    start := time.Now()
    msg := tgbotapi.NewMessage(chatID, text)
    _, err := bot.Send(msg)
    log.Printf("⏱️ sendMessage to %d took %v (error: %v)", chatID, time.Since(start), err)
}
