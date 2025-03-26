package repo

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisSessionStorage struct {
	client *redis.Client
}

func NewSessionRedisStorage(redis *redis.Client) *RedisSessionStorage {
	return &RedisSessionStorage{
		client: redis,
	}
}

func (r RedisSessionStorage) GetUserIdBySession(sessionID string) (string, bool) {
	v, err := r.client.Get(context.Background(), sessionID).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", false
		}
		slog.Error(err.Error())
		return "", false
	}

	return v, true
}

func (r RedisSessionStorage) StoreSession(sessionID string, userID string) {
	err := r.client.Set(context.Background(), sessionID, userID, time.Hour*11).Err()
	if err != nil {
		slog.Error("Ошибка записи сессии в Redis: " + err.Error())
	}
}

func (r RedisSessionStorage) DeleteSession(sessionID string) bool {
	err := r.client.Del(context.Background(), sessionID).Err()
	if err != nil {
		slog.Error("Ошибка удаления сессии из Redis: " + err.Error())
		return false
	}
	return true
}
