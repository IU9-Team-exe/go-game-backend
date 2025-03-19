package usecase

import (
	"context"
	"team_exe/internal/domain"
)

type KatagoStore interface {
	GenerateMove(ctx context.Context, moves []string) (string, error)
}

type KatagoUseCase struct {
	store KatagoStore
}

func NewKatagoUseCase(store KatagoStore) *KatagoUseCase {
	return &KatagoUseCase{
		store: store,
	}
}

func (k *KatagoUseCase) GenMove(ctx context.Context, moves []domain.Move) (domain.Move, error) {
	movesStrings := extractCoordinates(moves)

	botMove, err := k.store.GenerateMove(ctx, movesStrings)
	if err != nil {
		return domain.Move{}, err
	}

	return domain.Move{
		Coordinates: botMove,
		Color:       "w",
	}, nil
}

func extractCoordinates(moves []domain.Move) []string {
	coords := make([]string, 0)
	for _, m := range moves {
		coords = append(coords, m.Coordinates)
	}
	return coords
}
