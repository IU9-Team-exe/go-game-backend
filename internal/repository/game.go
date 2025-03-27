package repo

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
	"net/http"
	"team_exe/internal/bootstrap"
	"team_exe/internal/domain/game"
	"team_exe/internal/statuses"
	"time"
)

type GameRepository struct {
	cfg    bootstrap.Config
	log    *zap.SugaredLogger
	redis  *redis.Client
	mongo  *mongo.Database
	client *http.Client
}

func NewGameRepository(cfg bootstrap.Config, log *zap.SugaredLogger, redis *redis.Client, mongo *mongo.Database) *GameRepository {
	return &GameRepository{
		cfg:    cfg,
		log:    log,
		redis:  redis,
		mongo:  mongo,
		client: &http.Client{},
	}
}

func (g *GameRepository) GenerateGameKeys(ctx context.Context) (gameKeySecret string, gameKeyPublic string) {
	gameKeySecret = uuid.New().String()
	for {
		gameKeyPublic = generateHash(gameKeySecret)

		if g.CheckPublicKeyIsUniq(ctx, gameKeyPublic) {
			return gameKeySecret, gameKeyPublic
		}
	}
}

func generateHash(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	hashBytes := h.Sum(nil)
	number := binary.BigEndian.Uint32(hashBytes[:4])
	code := number % 100000
	return fmt.Sprintf("%05d", code)
}

func (g *GameRepository) CheckPublicKeyIsUniq(ctx context.Context, gameKeyPublic string) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	collection := g.mongo.Collection("games")
	filter := bson.M{
		"game_key_public": gameKeyPublic,
	}
	err := collection.FindOne(ctx, filter).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return true
	}
	return false
}

func (g *GameRepository) PutGameToMongoDatabase(ctx context.Context, gameData game.Game) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	collection := g.mongo.Collection("games")

	_, err := collection.InsertOne(ctx, gameData)
	if err != nil {
		g.log.Errorf("failed to insert game to database: %v", err)
		return false
	}

	g.log.Infof("game inserted successfully with key: %s", gameData.GameKeySecret)

	return true
}

func (g *GameRepository) AddPlayer(ctx context.Context, newUser game.GameUser, gameKey string) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	collection := g.mongo.Collection("games")

	filter := bson.M{"game_key": gameKey}

	update := bson.M{}
	if newUser.Color == "white" {
		update = bson.M{
			"$push": bson.M{
				"users": newUser,
			},
			"$set": bson.M{
				"player_white": newUser.ID,
			},
		}
	} else {
		update = bson.M{
			"$push": bson.M{
				"users": newUser,
			},
			"$set": bson.M{
				"player_black": newUser.ID,
			},
		}
	}

	opts := options.Update().SetUpsert(false)

	res, err := collection.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		g.log.Errorf("failed to update game to database: %v", err)
		return false
	}

	if res.MatchedCount == 0 {
		g.log.Infof("игра с ключом %s не найдена", gameKey)
	}

	g.log.Infof("Пользователь добавлен к игре с ключом %s", gameKey)

	return true
}

func (g *GameRepository) GetUserByID(ctx context.Context, userID string) game.GameUser {

	// логика получения юзера

	user := game.GameUser{}
	user.ID = joinRequest.UserID
	user.Role = joinRequest.Role
	user.Color = g.CalculateUserColor(ctx, joinRequest.GameKey, joinRequest.UserID)
	return user
}

func (g *GameRepository) CalculateUserColor(ctx context.Context, gameKey string, userID string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	collection := g.mongo.Collection("games")

	filter := bson.M{"game_key": gameKey}

	var result game.Game
	err := collection.FindOne(ctx, filter).Decode(&result)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			g.log.Error("игра с ID %s не найдена", gameKey)
		}
		return ""
	}

	colorOfOpponent := ""
	for _, user := range result.Users {
		if user.ID != userID {
			colorOfOpponent = user.Color
		}
	}

	if colorOfOpponent == "black" {
		return "white"
	}
	return "black"
}

func (g *GameRepository) GetGameByGameKey(ctx context.Context, gameKey string) game.Game {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	collection := g.mongo.Collection("games")

	filter := bson.M{"game_key": gameKey}

	var result game.Game
	err := collection.FindOne(ctx, filter).Decode(&result)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			g.log.Error("игра с ID %s не найдена", gameKey)
		}
	}

	return result
}

func (g *GameRepository) SaveSGFToRedis(key string, sgfText string) error {
	ctx := context.Background()
	return g.redis.Set(ctx, key, sgfText, 0).Err()
}

func (g *GameRepository) LoadSGFFromRedis(key string) (string, error) {
	ctx := context.Background()
	return g.redis.Get(ctx, key).Result()
}

func (g *GameRepository) GetAllActiveGames() ([]game.Game, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	collection := g.mongo.Collection("games")
	filter := bson.M{
		"statuses": "active",
	}
	var result []game.Game
	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		g.log.Error(err)
		return result, err
	}

	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var play game.Game
		err = cursor.Decode(&play)
		if err != nil {
			g.log.Error(err)
			return result, err
		}
		result = append(result, play)
	}

	return result, nil
}

func (g *GameRepository) HasUserActiveGameByUserId(ctx context.Context, userID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	collection := g.mongo.Collection("games")
	filter := bson.M{
		"$and": []bson.M{
			{
				"$or": []bson.M{
					{"player_black": userID},
					{"player_white": userID},
				},
			},
			{
				"status": bson.M{
					"$ne": statuses.StatusCompleted,
				},
			},
		},
	}
	err := collection.FindOne(ctx, filter).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	} else if err != nil {
		g.log.Error(err)
		return false, err
	}

	return true, nil
}
