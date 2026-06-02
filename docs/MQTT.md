# MQTT в 2MOV


# MQTT в проекте 2MOV

## Брокер

- Адрес задаётся через переменную окружения `MQTT_BROKER` в `.env`
- Пример: `MQTT_BROKER=tcp://62.181.53.145:1883`
- Если переменная не задана, используется fallback: `tcp://62.181.53.145:1883`

## Клиенты

| Клиент | ClientID | Подписки |
|--------|----------|----------|
| MAX-клиент | `2mov_client` | `status/+`, `chat/+` |
| MAX-водитель | `2mov_driver` | `chat/+` |
| Telegram-бот | `2mov_telegram` | `status/+`, `chat/+` |

## Топики

| Топик | Формат | Отправитель | Получатель | QoS |
|-------|--------|-------------|-------------|-----|
| `chat/{userID}` | Текст | Любой бот | Клиент/водитель | 1 |
| `status/{userID}` | `accepted`, `at_pickup`, `to_delivery`, `at_delivery`, `completed` | Водитель | Клиент | 1 |




## Для связи серверов (Россия ↔  Зарубеж) используется мост:

 Пример

```
connection firstvds
address 62.181.53.145:1883
topic # both 0
cleansession true

```


Настройки моста находятся в `/etc/mosquitto/conf.d/bridge.conf` на HostVDS.

## Безопасность

- Доступ к MQTT ограничен iptables (только localhost и IP второго сервера)
- Адрес брокера вынесен в `.env`, не захардкожен

## Проверка

```bash
# Прослушивание всех топиков
`mosquitto_sub -h localhost -t "#" -v`

# Прослушивание статусов
`mosquitto_sub -h localhost -t "status/+" -v`

# Прослушивание чата
`mosquitto_sub -h localhost -t "chat/+" -v`


### Зависимости

    Клиенты используют библиотеку github.com/eclipse/paho.mqtt.golang

    Версия: v1.4.3 (совместима с Go 1.21+)

    Адрес брокера читается из os.Getenv("MQTT_BROKER")
