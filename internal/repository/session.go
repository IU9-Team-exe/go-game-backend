package repo

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisSessionStorage struct {
	client *redis.Client
}

func NewSessionRedisStorage(redis *redis.Client) *RedisSessionStorage {
	c := &RedisSessionStorage{
		client: redis,
	}
	return c
}

func (r RedisSessionStorage) GetUserIdBySession(sessionID string) (userID int, ok bool) {
	v, err := r.client.Get(context.Background(), sessionID).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return -1, false
		}
		slog.Error(err.Error())
		return -1, false
	}
	res, err := strconv.Atoi(v)
	if err != nil {
		slog.Error(err.Error())
		return -1, false
	}
	return res, true
}

//TODO проверять что редис живой

func (r RedisSessionStorage) StoreSession(sessionID string, userID int) {
	r.client.Set(context.Background(), sessionID, userID, time.Hour*11)
}

func (r RedisSessionStorage) DeleteSession(sessionID string) (ok bool) {
	r.client.Del(context.Background(), sessionID)
	return true
}
