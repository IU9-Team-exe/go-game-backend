package repository

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
	"net/http"
	"team_exe/internal/bootstrap"
	"team_exe/internal/domain/game"
	"team_exe/internal/domain/user"
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

func (g *GameRepository) AddPlayer(ctx context.Context, userId string, gameKey string) (game.Game, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	collection := g.mongo.Collection("games")

	filter := bson.M{"game_key": gameKey}

	update := bson.M{}

	userColor := g.CalculateUserColor(ctx, gameKey, userId)
	if userColor == "white" {
		update = bson.M{
			"$set": bson.M{
				"player_white": userId,
			},
		}
	} else if userColor == "black" {
		update = bson.M{
			"$set": bson.M{
				"player_black": userId,
			},
		}
	}

	opts := options.Update().SetUpsert(false)

	res, err := collection.UpdateOne(ctx, filter, update, opts)
	if err != nil {
		g.log.Errorf("failed to update game to database: %v", err)
		return game.Game{}, false
	}

	if res.MatchedCount == 0 {
		g.log.Infof("игра с ключом %s не найдена", gameKey)
	}

	var updatedGame game.Game
	err = collection.FindOne(ctx, filter).Decode(&updatedGame)
	if err != nil {
		g.log.Errorf("ошибка при получении обновлённой игры: %v", err)
		return game.Game{}, false
	}

	g.log.Infof("Пользователь %s (%s) добавлен к игре %s", userId, userColor, gameKey)

	return updatedGame, true
}

func (g *GameRepository) GetGameByPublicKey(ctx context.Context, gameKeyPublic string) (game.Game, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	collection := g.mongo.Collection("games")
	filter := bson.M{
		"$and": []bson.M{
			{
				"game_key_public": gameKeyPublic,
			},
			{
				"status": bson.M{
					"$ne": statuses.StatusCompleted,
				},
			},
		},
	}

	foundGame := game.Game{}

	err := collection.FindOne(ctx, filter).Decode(&foundGame)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return foundGame, nil
	} else if err != nil {
		g.log.Error(err)
		return foundGame, err
	}

	return foundGame, nil
}

func (g *GameRepository) GetUserByID(ctx context.Context, userID string) (user.User, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	idStr := "67e66998ecd55bbca68bc38b"

	// конвертируем её в ObjectID
	userIdObj, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		return user.User{}, err
	}

	filter := bson.M{"_id": userIdObj}
	collection := g.mongo.Collection("users")

	var result user.User
	err = collection.FindOne(ctx, filter).Decode(&result)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			g.log.Errorf("юзер с ID %s не найден", userID)
			return user.User{}, fmt.Errorf("user with id %s not found", userID)
		}
	}

	return result, nil
}

func (g *GameRepository) LeaveGameBySecretKey(ctx context.Context, secretKey string, userID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	collection := g.mongo.Collection("games")
	filter := bson.M{
		"game_key": secretKey,
	}

	var foundGame game.Game
	err := collection.FindOne(ctx, filter).Decode(&foundGame)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return fmt.Errorf("игры с id %s не найдено", secretKey)
	}
	if err != nil {
		g.log.Error("ошибка при поиске игры:", err)
		return err
	}
	updateFields := bson.M{}
	if foundGame.PlayerBlack == userID {
		updateFields["player_black"] = ""
	}
	if foundGame.PlayerWhite == userID {
		updateFields["player_white"] = ""
	}

	if len(updateFields) > 0 {
		_, err = collection.UpdateOne(
			ctx,
			bson.M{"game_key": foundGame.GameKeySecret},
			bson.M{"$set": updateFields},
		)
		if err != nil {
			g.log.Error("ошибка при апдейте игры:", err)
			return err
		}
	}

	return nil
}

func (g *GameRepository) CalculateUserColor(ctx context.Context, gameKey string, userID string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	collection := g.mongo.Collection("games")

	filter := bson.M{"game_key": gameKey}

	var foundGame game.Game
	err := collection.FindOne(ctx, filter).Decode(&foundGame)

	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			g.log.Error("игра с ID %s не найдена", gameKey)
		}
		return ""
	}

	if foundGame.PlayerBlack != "" {
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

func (g *GameRepository) GetActiveGameByUserId(ctx context.Context, userID string) (game.Game, error) {
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

	play := game.Game{}
	err := collection.FindOne(ctx, filter).Decode(&play)
	if errors.Is(err, mongo.ErrNoDocuments) {
		g.log.Error(fmt.Errorf("No active game found for user %s", userID))
		return play, fmt.Errorf("No active game found for user %s", userID)
	} else if err != nil {
		g.log.Error(err)
		return play, err
	}

	return play, nil
}

func (g *GameRepository) GetArchiveGamesByName(ctx context.Context, name string, pageNum int) (*game.ArchiveResponse, error) {
	filter := bson.M{
		"$or": bson.A{
			bson.M{"black_player": name},
			bson.M{"white_player": name},
		},
	}

	sort := bson.D{{"date", -1}}

	matchedGames, countOfAllGames, err := g.FetchGames(ctx, pageNum, filter, sort)
	if err != nil {
		return nil, err
	}

	return &game.ArchiveResponse{
		Games:             matchedGames,
		Page:              pageNum,
		TotalCountOfGames: countOfAllGames,
		PagesTotal:        (countOfAllGames + g.cfg.PageLimitGames - 1) / g.cfg.PageLimitGames,
	}, nil
}

func (g *GameRepository) GetArchiveGamesByYear(ctx context.Context, year int, pageNum int) (*game.ArchiveResponse, error) {
	filter := bson.M{}
	filter["date"] = bson.M{
		"$gte": time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC),
		"$lt":  time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	sort := bson.D{{"date", -1}}

	matchedGames, countOfAllGames, err := g.FetchGames(ctx, pageNum, filter, sort)
	if err != nil {
		return nil, err
	}

	return &game.ArchiveResponse{
		Games:             matchedGames,
		Page:              pageNum,
		TotalCountOfGames: countOfAllGames,
		PagesTotal:        (countOfAllGames + g.cfg.PageLimitGames - 1) / g.cfg.PageLimitGames,
	}, nil
}

func (g *GameRepository) FetchGames(ctx context.Context, pageNum int, filter bson.M, sort bson.D) ([]game.GameFromArchive, int, error) {
	coll := g.mongo.Collection("archive")

	total, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	opts := options.Find().
		SetSort(sort).
		SetSkip(int64((pageNum - 1) * g.cfg.PageLimitGames)).
		SetLimit(int64(g.cfg.PageLimitGames))

	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		fmt.Println(err)
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var games []game.GameFromArchive
	err = cursor.All(ctx, &games)
	if err != nil {
		fmt.Println(err)
		return nil, 0, err
	}

	fmt.Println(len(games))

	return games, int(total), nil
}

func (g *GameRepository) GetArchiveYears(ctx context.Context) (*game.ArchiveYearsResponse, error) {
	coll := g.mongo.Collection("archive")

	pipeline := mongo.Pipeline{
		{{Key: "$project", Value: bson.D{
			{Key: "year", Value: bson.D{
				{Key: "$year", Value: "$date"},
			}},
		}}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$year"},
			{Key: "count_of_games", Value: bson.D{
				{Key: "$sum", Value: 1},
			}},
		}}},
		{{Key: "$sort", Value: bson.D{
			{Key: "_id", Value: 1},
		}}},
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("aggregate error: %w", err)
	}
	defer cursor.Close(ctx)

	var rawResult []struct {
		Year         int `bson:"_id"`
		CountOfGames int `bson:"count_of_games"`
	}

	if err := cursor.All(ctx, &rawResult); err != nil {
		return nil, fmt.Errorf("cursor decoding error: %w", err)
	}

	response := &game.ArchiveYearsResponse{
		Years: make([]game.YearGameStruct, 0, len(rawResult)),
	}

	for _, item := range rawResult {
		response.Years = append(response.Years, game.YearGameStruct{
			Year:         item.Year,
			CountOfGames: item.CountOfGames,
		})
	}

	return response, nil
}

func (g *GameRepository) GetArchiveNames(ctx context.Context, pageNum int) (*game.ArchiveNamesResponse, error) {
	if pageNum < 1 {
		pageNum = 1
	}

	coll := g.mongo.Collection("archive")

	mainPipeline := mongo.Pipeline{
		{{Key: "$project", Value: bson.D{
			{Key: "player", Value: bson.A{"$black_player", "$white_player"}},
		}}},
		{{Key: "$unwind", Value: "$player"}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$player"},
			{Key: "count_of_games", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "count_of_games", Value: -1}}}},
		{{Key: "$skip", Value: (pageNum - 1) * g.cfg.PageLimitPlayers}},
		{{Key: "$limit", Value: g.cfg.PageLimitPlayers}},
	}

	cursor, err := coll.Aggregate(ctx, mainPipeline)
	if err != nil {
		return nil, fmt.Errorf("aggregate error: %w", err)
	}
	defer cursor.Close(ctx)

	var rawResult []struct {
		Name         string `bson:"_id"`
		CountOfGames int    `bson:"count_of_games"`
	}

	if err := cursor.All(ctx, &rawResult); err != nil {
		return nil, fmt.Errorf("cursor decoding error: %w", err)
	}

	countPipeline := mongo.Pipeline{
		{{Key: "$project", Value: bson.D{
			{Key: "player", Value: bson.A{"$black_player", "$white_player"}},
		}}},
		{{Key: "$unwind", Value: "$player"}},
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$player"},
		}}},
		{{Key: "$count", Value: "total"}},
	}

	countCursor, err := coll.Aggregate(ctx, countPipeline)
	if err != nil {
		return nil, fmt.Errorf("count aggregate error: %w", err)
	}
	defer countCursor.Close(ctx)

	var countResult []struct {
		Total int `bson:"total"`
	}

	if err := countCursor.All(ctx, &countResult); err != nil {
		return nil, fmt.Errorf("count decode error: %w", err)
	}

	total := 0
	if len(countResult) > 0 {
		total = countResult[0].Total
	}

	pagesTotal := (total + g.cfg.PageLimitPlayers - 1) / g.cfg.PageLimitPlayers // округление вверх

	response := &game.ArchiveNamesResponse{
		Names:             make([]game.NameGameStruct, 0, len(rawResult)),
		TotalCountOfNames: total,
		Page:              pageNum,
		PagesTotal:        pagesTotal,
	}

	for _, item := range rawResult {
		response.Names = append(response.Names, game.NameGameStruct{
			Name:         item.Name,
			CountOfGames: item.CountOfGames,
		})
	}

	return response, nil
}

func (g *GameRepository) GetGameFromArchiveById(ctx context.Context, gameFromArchiveById string) (*game.GameFromArchive, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	collection := g.mongo.Collection("archive")

	objectID, err := primitive.ObjectIDFromHex(gameFromArchiveById)
	if err != nil {
		g.log.Error("Invalid ObjectID:", err)
		return nil, err
	}

	filter := bson.M{
		"_id": objectID,
	}

	foundGame := &game.GameFromArchive{}

	err = collection.FindOne(ctx, filter).Decode(&foundGame)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return foundGame, nil
	} else if err != nil {
		g.log.Error(err)
		return foundGame, err
	}

	return foundGame, nil
}
