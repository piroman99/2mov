# MQTT в 2MOV

## Брокер
- Адрес: `tcp://62.181.53.145:1883`
- Контейнер: `mosquitto`
- Запуск: `docker run -d --name mosquitto -p 1883:1883 eclipse-mosquitto`

## Топики

| Топик | Формат | Отправитель | Получатель | QoS |
|-------|--------|-------------|------------|-----|
| `chat/{userID}` | Текст сообщения | Любой бот | Клиент/водитель | 1 |
| `status/{userID}` | `accepted`, `at_pickup`, `to_delivery`, `at_delivery`, `completed` | Водитель | Клиент | 1 |

## Клиенты

- `2mov_client` — MAX-клиентский бот
- `2mov_driver` — MAX-водительский бот
- `2mov_telegram` — Telegram-бот (скоро)

## Отладка

```bash
# Слушать всё
mosquitto_sub -h localhost -t "#" -v

# Слушать чаты
mosquitto_sub -h localhost -t "chat/+" -v

# Слушать статусы
mosquitto_sub -h localhost -t "status/+" -v
```
