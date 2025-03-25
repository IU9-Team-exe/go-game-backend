package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"net/http"
	"team_exe/internal/domain"

	"team_exe/internal/adapters"
	"team_exe/internal/bootstrap"
)

type KatagoRepository struct {
	cfg       *bootstrap.Config
	log       *zap.SugaredLogger
	redis     *adapters.AdapterRedis
	mongo     *adapters.AdapterMongo
	kataGoURL string
	client    *http.Client
}

func NewKatagoRepository(cfg *bootstrap.Config, log *zap.SugaredLogger) *KatagoRepository {
	kataGoURL := cfg.KatagoBotUrl
	return &KatagoRepository{
		cfg:       cfg,
		log:       log,
		redis:     adapters.NewAdapterRedis(cfg),
		mongo:     adapters.NewAdapterMongo(cfg),
		kataGoURL: kataGoURL,
		client:    &http.Client{},
	}
}

func generateUUID() string {
	return uuid.New().String()
}

type SelectMoveRequest struct {
	BoardSize int      `json:"board_size"`
	Moves     []string `json:"moves"`
}

func (k *KatagoRepository) GenerateMove(ctx context.Context, moves []string) (domain.BotResponse, error) {
	reqBody, err := json.Marshal(SelectMoveRequest{
		BoardSize: 19,
		Moves:     moves,
	})
	if err != nil {
		return domain.BotResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.kataGoURL, bytes.NewBuffer(reqBody))
	k.log.Info(req)
	if err != nil {
		return domain.BotResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := k.client.Do(req)
	if err != nil {
		return domain.BotResponse{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return domain.BotResponse{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result domain.BotResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return domain.BotResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}
