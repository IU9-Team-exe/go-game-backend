package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"log/slog"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"team_exe/internal/adapters"
	"team_exe/internal/domain/user"
	errs "team_exe/internal/errors"
)

// MongoUserStorage реализует интерфейс UserStorage для работы с MongoDB.
type MongoUserStorage struct {
	adapter *adapters.AdapterMongo
}

// NewMongoUserStorage конструктор хранилища пользователей на MongoDB.
func NewMongoUserStorage(adapter *adapters.AdapterMongo) *MongoUserStorage {
	return &MongoUserStorage{adapter: adapter}
}

// CheckExists проверяет, существует ли пользователь с заданным username.
func (m *MongoUserStorage) CheckExists(username string) bool {
	_, ok := m.GetUser(username)
	return ok
}

// GetUser ищет пользователя по username.
func (m *MongoUserStorage) GetUser(username string) (user.User, bool) {
	collection := m.adapter.Database.Collection("users")
	filter := bson.D{{Key: "username", Value: username}}

	var result user.User
	err := collection.FindOne(context.TODO(), filter).Decode(&result)
	if err != nil {
		if !errors.Is(err, mongo.ErrNoDocuments) {
			slog.Error("GetUser error: ", err)
		}
		return user.User{}, false
	}
	return result, true
}

// CreateUser создает новую запись пользователя.
// Может вернуть errors.ErrUserExists, если такой username уже существует.
func (m *MongoUserStorage) CreateUser(username, email, password string) (user.User, error) {
	// Проверяем, что пользователя ещё не существует.
	_, found := m.GetUser(username)
	if found {
		return user.User{}, errs.ErrUserExists
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
		slog.Error("CreateUser error: ", err)
		return user.User{}, errs.ErrInternal
	}

	// Преобразуем ObjectID → hex-строку и сохраняем в поле ID.
	newUser.ID = result.InsertedID.(primitive.ObjectID).Hex()
	return newUser, nil
}

// GetUserByID возвращает пользователя по его ID (Hex-строка ObjectID).
func (m *MongoUserStorage) GetUserByID(ctx context.Context, userID string) (user.User, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	userObjID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return user.User{}, fmt.Errorf("invalid userID format: %w", err)
	}

	filter := bson.M{"_id": userObjID}
	collection := m.adapter.Database.Collection("users")

	var result user.User
	if err = collection.FindOne(ctx, filter).Decode(&result); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return user.User{}, fmt.Errorf("user with id %s not found", userID)
		}
		return user.User{}, err
	}

	return result, nil
}

func (m *MongoUserStorage) AddLose(ctx context.Context, userID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	userObjID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return fmt.Errorf("invalid userID format: %w", err)
	}

	filter := bson.M{"_id": userObjID}
	collection := m.adapter.Database.Collection("users")

	var result user.User
	if err = collection.FindOne(ctx, filter).Decode(&result); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return fmt.Errorf("user with id %s not found", userID)
		}
		return err
	}
	result.Statistic.Losses++
	update := bson.D{{"$set", bson.D{{"statistic", result.Statistic}}}}
	_, err = collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	return nil
}
