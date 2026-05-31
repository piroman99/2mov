package mqtt

import (
    "log"
    "os"
    "strings"

    mqtt "github.com/eclipse/paho.mqtt.golang"
    "2mov-bot-driver/internal/utils"
)

var Client mqtt.Client

func Init() {
    broker := os.Getenv("MQTT_BROKER")
    if broker == "" {
        broker = "tcp://62.181.53.145:1883"
    }
    opts := mqtt.NewClientOptions()
    opts.AddBroker(broker)
    opts.SetClientID("2mov_driver")
    opts.SetCleanSession(true)
    opts.SetAutoReconnect(true)

    Client = mqtt.NewClient(opts)
    if token := Client.Connect(); token.Wait() && token.Error() != nil {
        log.Printf("⚠️ MQTT connect error: %v", token.Error())
        return
    }
    log.Println("✅ MQTT connected")
}

func SubscribeToChat(token string) {
    if Client == nil || !Client.IsConnected() {
        return
    }
    Client.Subscribe("chat/+", 1, func(c mqtt.Client, m mqtt.Message) {
        parts := strings.Split(m.Topic(), "/")
        if len(parts) == 2 {
            driverID := parts[1]
            log.Printf("📡 MQTT received for driver %s: %s", driverID, m.Payload())
            utils.SendMessage(token, driverID, string(m.Payload()))
        }
    })
    log.Println("✅ MQTT driver subscribed to chat/+")
}
