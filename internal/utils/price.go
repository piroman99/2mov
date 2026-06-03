package utils

import (
    "math"
)

func SimpleDistance(lat1, lon1, lat2, lon2 float64) float64 {
    const earthRadius = 6371.0
    dLat := (lat2 - lat1) * math.Pi / 180
    dLon := (lon2 - lon1) * math.Pi / 180
    a := math.Sin(dLat/2)*math.Sin(dLat/2) +
        math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
            math.Sin(dLon/2)*math.Sin(dLon/2)
    c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
    return earthRadius * c
}

func CalculatePrice(distance float64) float64 {
    basePrice := 200.0
    pricePerKm := 30.0
    return basePrice + distance*pricePerKm
}
