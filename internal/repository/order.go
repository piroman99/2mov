package repository

import (
    "context"
    "time"

    "go.mongodb.org/mongo-driver/bson/primitive"
    "go.mongodb.org/mongo-driver/mongo"
    "2mov/internal/models"
)

func SaveOrder(db *mongo.Database, order models.Order) error {
    collection := db.Collection("orders")
    order.ID = primitive.NewObjectID().Hex()
    order.CreatedAt = time.Now()
    order.Status = "pending"
    _, err := collection.InsertOne(context.Background(), order)
    return err
}
