package models

import "time"

type User struct {
    ID           string    `bson:"_id,omitempty"`
    MaxUserID    int       `bson:"max_user_id,omitempty"`
    TelegramID   string    `bson:"telegram_id,omitempty"`
    FirstName    string    `bson:"first_name"`
    LastName     string    `bson:"last_name"`
    Username     string    `bson:"username"`
    Role         string    `bson:"role"`
    Rating       float64   `bson:"rating"`
    TripsCount   int       `bson:"trips_count"`
    CreatedAt    time.Time `bson:"created_at"`
    LastActiveAt time.Time `bson:"last_active_at"`
}

type Session struct {
    UserID      string    `bson:"user_id"`
    Step        string    `bson:"step"`
    FromAddress string    `bson:"from_address"`
    FromLat     float64   `bson:"from_lat"`
    FromLon     float64   `bson:"from_lon"`
    ToAddress   string    `bson:"to_address"`
    ToLat       float64   `bson:"to_lat"`
    ToLon       float64   `bson:"to_lon"`
    UpdatedAt   time.Time `bson:"updated_at"`
}

type Order struct {
    ID          string    `bson:"_id,omitempty"`
    ClientID    string    `bson:"client_id"`
    FromAddress string    `bson:"from_address"`
    FromLat     float64   `bson:"from_lat"`
    FromLon     float64   `bson:"from_lon"`
    ToAddress   string    `bson:"to_address"`
    ToLat       float64   `bson:"to_lat"`
    ToLon       float64   `bson:"to_lon"`
    Price       float64   `bson:"price"`
    Status      string    `bson:"status"`
    DriverID    string    `bson:"driver_id,omitempty"`
    CreatedAt   time.Time `bson:"created_at"`
    CancelledBy string    `bson:"cancelled_by,omitempty"`
    CancelledAt time.Time `bson:"cancelled_at,omitempty"`
}
