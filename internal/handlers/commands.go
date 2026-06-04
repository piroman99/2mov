package handlers

import (
    "fmt"
    "2mov/internal/constants"
    "2mov/internal/utils"
)

func HandleStart(token, userID, firstName string, rating float64) {
    reply := constants.TestModeNotice + fmt.Sprintf("🚕 Добро пожаловать в 2MOV, %s!\nВаш рейтинг: %.1f\nОтправьте /help", firstName, rating) + constants.HelpFooter
    utils.SendMessage(token, userID, reply)
}

func HandleHelp(token, userID string) {
    reply := constants.TestModeNotice + "📋 Доступные команды:\n/start — начало\n/help — справка\n/profile — мой профиль\n/order — создать заказ" + constants.HelpFooter
    utils.SendMessage(token, userID, reply)
}

func HandleProfile(token, userID, firstName, lastName string, rating float64, tripsCount int) {
    reply := fmt.Sprintf("👤 %s %s\n⭐ Рейтинг: %.1f\n🚕 Поездок: %d", firstName, lastName, rating, tripsCount)
    utils.SendMessage(token, userID, reply)
}

func HandleOrder(token, userID string) {
    utils.SendMessage(token, userID, "📍 Отправьте точку отправления (геолокацию или адрес)")
}
