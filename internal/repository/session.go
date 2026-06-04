package repository

import (
    "context"

    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/mongo"
    "go.mongodb.org/mongo-driver/mongo/options"
    "2mov/internal/models"
)

func SaveSession(db *mongo.Database, session models.Session) {
    collection := db.Collection("sessions")
    opts := options.Update().SetUpsert(true)
    filter := bson.M{"user_id": session.UserID}
    update := bson.M{"$set": session}
    collection.UpdateOne(context.Background(), filter, update, opts)
}

func GetSession(db *mongo.Database, userID string) (models.Session, error) {
    var session models.Session
    collection := db.Collection("sessions")
    err := collection.FindOne(context.Background(), bson.M{"user_id": userID}).Decode(&session)
    return session, err
}

func DeleteSession(db *mongo.Database, userID string) {
    collection := db.Collection("sessions")
    collection.DeleteOne(context.Background(), bson.M{"user_id": userID})
}
