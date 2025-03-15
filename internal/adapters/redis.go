package adapters

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"team_exe/internal/bootstrap"
)

type AdapterRedis struct {
	client *redis.Client
	cfg    *bootstrap.Config
}

func NewAdapterRedis(cfg *bootstrap.Config) *AdapterRedis {
	return &AdapterRedis{
		cfg: cfg,
	}
}

func (a *AdapterRedis) Init(ctx context.Context) error {
	addr := "localhost:6379" // Укажите ваш адрес Redis
	password := ""           // Укажите пароль, если есть
	a.client = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := a.client.Ping(ctx).Err(); err != nil {
		log.Fatalf("Ошибка подключения к Redis: %v", err)
		return err
	}

	log.Println("Успешно подключено к Redis")
	return nil
}

func (a *AdapterRedis) GetClient() *redis.Client {
	return a.client
}
