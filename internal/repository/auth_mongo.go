package repo

import (
	"context"
	"errors"
	"log/slog"
	"team_exe/internal/adapters"
	"team_exe/internal/domain/user"
	errors2 "team_exe/internal/errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type MongoUserStorage struct {
	adapter *adapters.AdapterMongo
}

func NewMongoUserStorage(adapter *adapters.AdapterMongo) *MongoUserStorage {
	return &MongoUserStorage{adapter: adapter}
}

func (m MongoUserStorage) CheckExists(username string) bool {
	_, ok := m.GetUser(username)
	return ok
}

func (m MongoUserStorage) GetUser(username string) (user.User, bool) {
	collection := m.adapter.Database.Collection("users")
	filter := bson.D{{"username", username}}

	var result user.User
	err := collection.FindOne(context.TODO(), filter).Decode(&result)
	if err != nil {
		if !errors.Is(err, mongo.ErrNoDocuments) {
			slog.Error(err.Error())
		}
		return user.User{}, false
	}
	return result, true
}

func (m MongoUserStorage) GetUserByID(id string) (user.User, bool) {
	collection := m.adapter.Database.Collection("users")
	filter := bson.D{{"_id", id}}

	var result user.User
	err := collection.FindOne(context.TODO(), filter).Decode(&result)
	if err != nil {
		if !errors.Is(err, mongo.ErrNoDocuments) {
			slog.Error(err.Error())
		}
		return user.User{}, false
	}
	return result, true
}

func (m MongoUserStorage) CreateUser(username, email, password string) (user.User, error) {
	_, found := m.GetUser(username)
	if found {
		return user.User{}, errors2.ErrUserExists
	}
	collection := m.adapter.Database.Collection("users")
	newUser := user.User{
		Username:       username,
		Email:          email,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		Rating:         0,
		CurrentGameKey: "",
		AvatarURL:      "",
		Status:         "",
		Statistic: user.UserStatistic{
			Wins:         0,
			Losses:       0,
			Draws:        0,
			Achievements: nil,
		},
		PasswordHash: password,
	}
	result, err := collection.InsertOne(context.TODO(), newUser)
	if err != nil {
		slog.Error(err.Error())
		return user.User{}, errors2.ErrInternal
	}
	newUser.ID = result.InsertedID.(primitive.ObjectID).String()
	return newUser, nil
}
