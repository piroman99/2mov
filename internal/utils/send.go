package utils

import (
    "bytes"
    "encoding/json"
    "fmt"
    "net/http"
)

func SendMessage(token, chatID, text string) {
    url := fmt.Sprintf("https://platform-api.max.ru/messages?user_id=%s", chatID)
    payload := map[string]interface{}{"text": text}
    jsonData, _ := json.Marshal(payload)
    req, _ := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", token)
    http.DefaultClient.Do(req)
}

func SendMessageWithButtons(token, chatID, text string, buttons [][]map[string]interface{}) {
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
