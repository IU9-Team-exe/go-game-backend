package adapters

import (
	"context"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"team_exe/internal/bootstrap"
)

type AdapterMongo struct {
	*mongo.Database
	cfg *bootstrap.Config
}

func NewAdapterMongo(cfg *bootstrap.Config) *AdapterMongo {
	return &AdapterMongo{
		cfg: cfg,
	}
}

func (a *AdapterMongo) Init(ctx context.Context) error {
	uri := "mongodb://root:Artem557@localhost:8082/team_exe?authSource=admin"

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatalf("Ошибка подключения к MongoDB: %v", err)
	}

	// Проверка подключения
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("Не удалось пропинговать MongoDB: %v", err)
	}

	a.Database = client.Database("team_exe")

	log.Println("Успешно подключено к MongoDB")
	return nil
}
