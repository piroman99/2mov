# Кнопки в MAX Bot API (краткая документация)

## 1. Типы кнопок

- callback — отправляет callback на сервер (поле callback в вебхуке)
- link — открывает URL (поле url)
- request_location — запрашивает геолокацию (поле request_location: true)
- request_contact — запрашивает контакт (поле request_contact: true)

## 2. Отправка кнопок
```
{
  "text": "Выберите действие:",
  "attachments": [
    {
      "type": "inline_keyboard",
      "payload": {
        "buttons": [
          [
            { "type": "callback", "text": "✅ Да", "payload": "confirm" },
            { "type": "callback", "text": "✏️ Изменить", "payload": "edit" }
          ]
        ]
      }
    }
  ]
}
```
## 3. Обработка callback (вебхук)

В вебхук приходит объект callback (не message_callback):
```
{
  "callback": {
    "callback_id": "...",
    "payload": "confirm",
    "user": { "user_id": 123 }
  },
  "message": { ... }
}
```
Код обработки:
```
if cb, ok := update["callback"].(map[string]interface{}); ok {
    callbackID := cb["callback_id"].(string)
    payload := cb["payload"].(string)

    answerURL := fmt.Sprintf("https://platform-api.max.ru/answers?callback_id=%s", callbackID)
    answerBody := map[string]interface{}{
        "notification": "Готово",
    }
    // POST запрос с Authorization: token, Content-Type: application/json
}
```
## 4. Важные нюансы

- Поле в вебхуке — callback, а не message_callback (ошибка в неофициальной документации)
- Ответ на callback обязателен, иначе MAX будет повторять запросы
- Если не передать message в ответе — текст сообщения не меняется (только уведомление)
- Если передать пустой attachments — кнопки исчезнут (нужно копировать исходные)
- Для отправки нового сообщения используйте обычный sendMessage, не через answers

## 5. Кнопка "Поделиться местоположением"
```
{ "type": "request_location", "text": "📍 Отправить геолокацию" }
```
Формат в вебхуке:
```
"attachments": [
  { "type": "location", "latitude": 55.751244, "longitude": 37.618423 }
]
```
## 6. Кнопка "Поделиться контактом"
```
{ "type": "request_contact", "text": "📱 Отправить контакт" }
```
Формат в вебхуке:
```
"attachments": [
  { "type": "contact", "first_name": "Иван", "last_name": "Карпухин", "phone_number": "+79001234567" }
]
```
## 7. Полный пример кнопок с геолокацией
```
{
  "text": "Отправьте местоположение:",
  "attachments": [
    {
      "type": "inline_keyboard",
      "payload": {
        "buttons": [
          [
            { "type": "request_location", "text": "📍 Поделиться геолокацией" }
          ],
          [
            { "type": "callback", "text": "✅ Да", "payload": "confirm" },
            { "type": "callback", "text": "✏️ Изменить", "payload": "edit" }
          ]
        ]
      }
    }
  ]
}
```
