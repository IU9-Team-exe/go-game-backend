package katago

import (
	"context"
	"fmt"
	"team_exe/internal/domain/game"
)

type KatagoStore interface {
	SendRequestToKatago(ctx context.Context, gameUniqID string, moves *game.Moves, boardSize int, rules string, ownership, policy bool) (*game.KataGoResponse, error)
}

type KatagoUseCase struct {
	store KatagoStore
}

func NewKatagoUseCase(store KatagoStore) *KatagoUseCase {
	return &KatagoUseCase{store: store}
}

func (k *KatagoUseCase) CheckMove(ctx context.Context, gameUniqId string, moves *game.Moves, boardSize int, rules string) (*game.KataGoResponse, error) {
	includeOwnership := false
	includePolicy := false
	resp, err := k.store.SendRequestToKatago(ctx, gameUniqId, moves, boardSize, rules, includeOwnership, includePolicy)
	if err != nil {
		return nil, err
	}

	if resp.ID != gameUniqId {
		return nil, fmt.Errorf("id of req and resp does not match: %s, %s", gameUniqId, resp.ID)
	}

	return resp, nil
}

func (k *KatagoUseCase) AnalyseCurrentGame(ctx context.Context, gameUniqId string, moves *game.Moves, boardSize int, rules string) (*game.KataGoResponse, error) {
	fmt.Println("MOVES:")
	fmt.Println(moves)

	includeOwnership := true
	includePolicy := true
	resp, err := k.store.SendRequestToKatago(ctx, gameUniqId, moves, boardSize, rules, includeOwnership, includePolicy)
	if err != nil {
		return nil, err
	}

	if resp.ID != gameUniqId {
		return nil, fmt.Errorf("id of req and resp does not match: %s, %s", gameUniqId, resp.ID)
	}

	return resp, nil
}

func (k *KatagoUseCase) GenerateMove(ctx context.Context, gameUniqId string, moves *game.Moves, boardSize int, rules string) (*game.MoveInfo, error) {
	includeOwnership := false
	includePolicy := false
	resp, err := k.store.SendRequestToKatago(ctx, gameUniqId, moves, boardSize, rules, includeOwnership, includePolicy)
	if err != nil {
		return nil, err
	}

	if len(resp.MoveInfos) == 0 {
		return nil, fmt.Errorf("Katago returned no move infos")
	}

	if resp.ID != gameUniqId {
		return nil, fmt.Errorf("id of req and resp does not match: %s, %s", gameUniqId, resp.ID)
	}

	return &resp.MoveInfos[0], nil
}
