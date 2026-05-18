package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
)

func main() {
    log.Println("🚀 2MOV бот запускается...")

    mode := os.Getenv("MODE")
    if mode == "" {
        mode = "max"
    }

    if mode == "telegram" {
        runTelegramBot()
    } else {
        runMaxBot()
    }
}

func getKeys(m map[string]interface{}) []string {
    keys := make([]string, 0, len(m))
    for k := range m {
        keys = append(keys, k)
    }
    return keys
}

func runMaxBot() {
    log.Println("🤖 Запуск MAX-бота...")

    token := os.Getenv("MAX_BOT_TOKEN")
    if token == "" {
        log.Fatal("❌ MAX_BOT_TOKEN не задан")
    }

    http.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
        body, err := io.ReadAll(r.Body)
        if err != nil {
            log.Printf("❌ Ошибка чтения тела: %v", err)
            w.WriteHeader(http.StatusBadRequest)
            return
        }
        defer r.Body.Close()

        if len(body) == 0 {
            log.Println("⚠️ Пустой вебхук (heartbeat), игнорируем")
            w.WriteHeader(http.StatusOK)
            return
        }

        log.Printf("📩 Получен вебхук: %s", string(body))

        var update map[string]interface{}
        if err := json.Unmarshal(body, &update); err != nil {
            log.Printf("❌ Ошибка парсинга JSON: %v", err)
            w.WriteHeader(http.StatusBadRequest)
            return
        }

        // Извлекаем текст сообщения
        var text string
        if msg, ok := update["message"].(map[string]interface{}); ok {
            if body, ok := msg["body"].(map[string]interface{}); ok {
                text, _ = body["text"].(string)
            }
        }

        // Извлекаем user_id отправителя из message.sender
        var recipientID int
        if msg, ok := update["message"].(map[string]interface{}); ok {
            if senderRaw, ok := msg["sender"]; ok {
                if sender, ok := senderRaw.(map[string]interface{}); ok {
                    if id, ok := sender["user_id"]; ok {
                        switch v := id.(type) {
                        case float64:
                            recipientID = int(v)
                        case int:
                            recipientID = v
                        }
                    }
                }
            }
        }

        if text == "" || recipientID == 0 {
            log.Printf("⚠️ Нет текста (%s) или recipient_id (%d), игнорируем", text, recipientID)
            w.WriteHeader(http.StatusOK)
            return
        }

        var reply string
        switch text {
        case "/start":
            reply = "🚕 Добро пожаловать в 2MOV!\nОтправьте /help для списка команд"
        case "/help":
            reply = "📋 Доступные команды:\n/start — начало\n/help — справка"
        default:
            reply = "Отправьте /help для списка команд"
        }

        go sendMaxMessage(token, recipientID, reply)
        w.WriteHeader(http.StatusOK)
    })

    go func() {
        log.Println("✅ HTTP-сервер запущен на :8080")
        if err := http.ListenAndServe(":8080", nil); err != nil {
            log.Printf("❌ Ошибка HTTP-сервера: %v", err)
        }
    }()

    log.Println("✅ MAX-бот готов к приёму вебхуков")

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    log.Println("👋 MAX-бот остановлен")
}

func sendMaxMessage(token string, recipientID int, text string) {
    url := fmt.Sprintf("https://platform-api.max.ru/messages?user_id=%d", recipientID)

    payload := map[string]interface{}{
        "text": text,
    }
    jsonData, err := json.Marshal(payload)
    if err != nil {
        log.Printf("❌ Ошибка маршалинга JSON: %v", err)
        return
    }

    req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
    if err != nil {
        log.Printf("❌ Ошибка создания запроса: %v", err)
        return
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Authorization", token)

    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        log.Printf("❌ Ошибка отправки сообщения: %v", err)
        return
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    log.Printf("📤 Ответ MAX API (status=%d): %s", resp.StatusCode, string(body))
}

func runTelegramBot() {
    log.Println("🤖 Запуск Telegram-бота...")
    log.Println("✅ Telegram-бот готов (заглушка)")
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit
    log.Println("👋 Telegram-бот остановлен")
}
