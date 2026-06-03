package mymqtt

import (
    "log"
    "os"

    mqtt "github.com/eclipse/paho.mqtt.golang"
)

var Client mqtt.Client

func Init() {
    broker := os.Getenv("MQTT_BROKER")
    if broker == "" {
        broker = "tcp://62.181.53.145:1883"
    }
    opts := mqtt.NewClientOptions()
    opts.AddBroker(broker)
    opts.SetClientID("2mov_client")
    opts.SetCleanSession(true)
    opts.SetAutoReconnect(true)

    Client = mqtt.NewClient(opts)
    if token := Client.Connect(); token.Wait() && token.Error() != nil {
        log.Printf("⚠️ MQTT connect error: %v", token.Error())
        return
    }
    log.Println("✅ MQTT connected")
}

func IsConnected() bool {
    return Client != nil && Client.IsConnected()
}

func Publish(topic string, qos byte, retained bool, payload interface{}) {
    if Client != nil && Client.IsConnected() {
        Client.Publish(topic, qos, retained, payload)
    }
}
