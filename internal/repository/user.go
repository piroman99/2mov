package repository

import (
    "context"
    "time"

    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/mongo"
    "2mov/internal/models"
)

func FindOrCreateUserByMaxID(db *mongo.Database, maxUserID int, firstName, lastName, username string) (*models.User, error) {
    collection := db.Collection("users")
    ctx := context.Background()
    var user models.User
    err := collection.FindOne(ctx, bson.M{"max_user_id": maxUserID}).Decode(&user)
    if err == nil {
        update := bson.M{"$set": bson.M{"last_active_at": time.Now()}}
        collection.UpdateOne(ctx, bson.M{"max_user_id": maxUserID}, update)
        return &user, nil
    }
    newUser := models.User{
        MaxUserID:    maxUserID,
        FirstName:    firstName,
        LastName:     lastName,
        Username:     username,
        Role:         "client",
        Rating:       5.0,
        TripsCount:   0,
        CreatedAt:    time.Now(),
        LastActiveAt: time.Now(),
    }
    _, err = collection.InsertOne(ctx, newUser)
    if err != nil {
        return nil, err
    }
    return &newUser, nil
}
