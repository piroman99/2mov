package utils

import (
    "fmt"
)

func ParseLocation(update map[string]interface{}) (address string, lat, lon float64, err error) {
    msg, ok := update["message"].(map[string]interface{})
    if !ok {
        return "", 0, 0, fmt.Errorf("no message")
    }
    if body, ok := msg["body"].(map[string]interface{}); ok {
        if attachments, ok := body["attachments"].([]interface{}); ok && len(attachments) > 0 {
            for _, att := range attachments {
                if attMap, ok := att.(map[string]interface{}); ok {
                    if attMap["type"] == "location" {
                        lat, _ = attMap["latitude"].(float64)
                        lon, _ = attMap["longitude"].(float64)
                        return fmt.Sprintf("%f,%f", lat, lon), lat, lon, nil
                    }
                }
            }
        }
        if text, ok := body["text"].(string); ok && text != "" {
            return text, 0, 0, nil
        }
    }
    return "", 0, 0, fmt.Errorf("no location")
}
