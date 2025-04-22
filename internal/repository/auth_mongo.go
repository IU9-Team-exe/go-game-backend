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
func (m *MongoUserStorage) CreateUser(username, email, password string, isGhost bool) (user.User, error) {
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
		IsGhost:        isGhost,
		Rating:         0,
		CurrentGameKey: "",
		AvatarURL:      "",
		Status:         "",
		Statistic: user.UserStatistic{
			Rating:       1500.0,
			Rd:           350.0,
			Volatility:   0.06,
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

func (m *MongoUserStorage) GetUserByUsername(ctx context.Context, username string) (user.User, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	filter := bson.M{"username": username}
	collection := m.adapter.Database.Collection("users")

	var result user.User
	if err := collection.FindOne(ctx, filter).Decode(&result); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return user.User{}, fmt.Errorf("user with nickname %s not found", username)
		}
		return user.User{}, err
	}

	return result, nil
}

func (m *MongoUserStorage) UpdateUser(ctx context.Context, user user.User, userID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	userObjID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return fmt.Errorf("invalid userID format: %w", err)
	}

	filter := bson.M{"_id": userObjID}
	collection := m.adapter.Database.Collection("users")

	update := bson.M{}
	if user.Username != "" {
		update["username"] = user.Username
	}
	if user.Email != "" {
		update["email"] = user.Email
	}
	if user.AvatarURL != "" {
		update["avatar_url"] = user.AvatarURL
	}

	if len(update) == 0 {
		return nil
	}

	res, err := collection.UpdateOne(ctx, filter, bson.M{"$set": update})
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return fmt.Errorf("user with id %s not found", userID)
		}
		return fmt.Errorf("failed to update user: %w", err)
	}

	if res.MatchedCount == 0 {
		return fmt.Errorf("user with id %s not found", userID)
	}

	return nil
}

func (m *MongoUserStorage) AddResult(ctx context.Context, userID string, isWin bool, oppRating, oppRd, oppVolatility float64) error {
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
	if isWin {
		result.Statistic.Wins++
	} else {
		result.Statistic.Losses++
	}
	result.Statistic.Games = append(result.Statistic.Games, user.GameResultElo{OppRating: oppRating, OppRd: oppRd, OppVolatility: oppVolatility, DidUserWin: isWin})
	update := bson.D{{"$set", bson.D{{"statistic", result.Statistic}}}}
	_, err = collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	return nil
}

func (m *MongoUserStorage) UpdateRating(ctx context.Context, userID string, rating, rd, volatility float64) error {
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
	result.Statistic.Rating = rating
	result.Statistic.Rd = rd
	result.Statistic.Volatility = volatility
	update := bson.D{{"$set", bson.D{{"statistic", result.Statistic}}}}
	_, err = collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	return nil
}
