package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"team_exe/internal/adapters"
	"team_exe/internal/bootstrap"
	"team_exe/internal/domain/game"
	errs "team_exe/internal/errors"
)

type KatagoStorage struct {
	cfg   *bootstrap.Config
	mongo *adapters.AdapterMongo
	redis *adapters.AdapterRedis
}

func NewKatagoStorage(cfg *bootstrap.Config, mongo *adapters.AdapterMongo, redis *adapters.AdapterRedis) *KatagoStorage {
	return &KatagoStorage{
		cfg:   cfg,
		mongo: mongo,
		redis: redis,
	}
}

func (k *KatagoStorage) SendRequestToKatago(ctx context.Context, gameUniqID string, moves *game.Moves, boardSize int, rules string, ownership, policy bool) (*game.KataGoResponse, error) {
	convertedMoves := ConvertMovesToKatagoFormat(moves)
	katagoReq := game.KatagoRequest{
		Id:               gameUniqID,
		BoardXSize:       boardSize,
		BoardYSize:       boardSize,
		Rules:            rules,
		Moves:            convertedMoves,
		IncludeOwnership: ownership,
		IncludePolicy:    policy,
	}

	reqBody, err := json.Marshal(katagoReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errs.ErrMarshal, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.cfg.KatagoBotUrl, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errs.ErrSendRequest, err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send katago request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to send katago request: %d %s", resp.StatusCode, string(body))
	}

	var katagoResp game.KataGoResponse
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&katagoResp); err != nil {
		return nil, fmt.Errorf("failed to decode katago response: %w", err)
	}

	return &katagoResp, nil

}

func ConvertMovesToKatagoFormat(moves *game.Moves) [][]string {
	movesSlice := make([][]string, len(moves.Moves))
	for i, move := range moves.Moves {
		movesSlice[i] = make([]string, 2)
		movesSlice[i][0] = move.Color
		movesSlice[i][1] = move.Coordinates
	}
	return movesSlice
}
