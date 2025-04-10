package adapters

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"team_exe/internal/bootstrap"
)

type AdapterMongo struct {
	Client   *mongo.Client
	Database *mongo.Database
	cfg      *bootstrap.Config
}

func NewAdapterMongo(cfg *bootstrap.Config) *AdapterMongo {
	return &AdapterMongo{
		cfg: cfg,
	}
}

func (a *AdapterMongo) Init(ctx context.Context) error {
	clientOpts := options.Client().ApplyURI(a.cfg.MongoUri)

	ctxConnect, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	Client, err := mongo.Connect(ctxConnect, clientOpts)
	if err != nil {
		return fmt.Errorf("ошибка подключения к MongoDB: %w", err)
	}

	if err = Client.Ping(ctx, nil); err != nil {
		log.Fatalf("Не удалось пропинговать MongoDB: %v", err)
	}

	a.Database = Client.Database("team_exe")
	err = a.InitIndexes(ctx)
	if err != nil {
		return err
	}

	log.Println("Успешно подключено к MongoDB")
	return nil
}

func (a *AdapterMongo) Close(ctx context.Context) error {
	if a.Client != nil {
		return a.Client.Disconnect(ctx)
	}
	return nil
}

func (a *AdapterMongo) InitIndexes(ctx context.Context) error {
	archiveColl := a.Database.Collection("archive")

	indexModels := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "date", Value: 1}}, // индекс по дате (в порядке возрастания)
		},
		{
			Keys: bson.D{{Key: "black_player", Value: 1}}, // индекс по чёрному игроку
		},
		{
			Keys: bson.D{{Key: "white_player", Value: 1}}, // индекс по белому игроку
		},
	}

	_, err := archiveColl.Indexes().CreateMany(ctx, indexModels)
	if err != nil {
		return fmt.Errorf("ошибка создания индексов: %w", err)
	}
	return nil
}
