package adapters

import (
	"context"
	"fmt"
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
	// Используем адрес из конфига
	addr := a.cfg.RedisUrl
	password := "" // если используешь пароль — добавь в cfg

	a.client = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})

	// Контекст с таймаутом
	ctxPing, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := a.client.Ping(ctxPing).Err(); err != nil {
		return fmt.Errorf("ошибка подключения к Redis: %w", err)
	}

	log.Println("Успешно подключено к Redis")
	return nil
}

func (a *AdapterRedis) GetClient() *redis.Client {
	return a.client
}

func (a *AdapterRedis) Close(ctx context.Context) error {
	if a.client != nil {
		return a.client.Close()
	}
	return nil
}
