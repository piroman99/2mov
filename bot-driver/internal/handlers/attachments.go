package handlers

import (
    "fmt"

    "2mov-bot-driver/internal/utils"
)

// HandleLocation обрабатывает геолокацию от водителя
func HandleLocation(token, userIDStr string, lat, lon float64) {
    link := fmt.Sprintf("📍 https://yandex.ru/maps/?pt=%f,%f&z=15", lon, lat)
    utils.SendMessage(token, userIDStr, link)
}

// HandleContact обрабатывает контакт от водителя
func HandleContact(token, userIDStr, firstName, lastName, phone string) {
    text := fmt.Sprintf("📱 %s %s\n%s", firstName, lastName, phone)
    utils.SendMessage(token, userIDStr, text)
}
