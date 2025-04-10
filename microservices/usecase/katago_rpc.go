package usecase

import (
	"context"
	"team_exe/internal/domain/game"
	katagoRPC "team_exe/microservices/proto"
)

type KatagoStore interface {
	GenerateMove(ctx context.Context, moves []string) (game.BotResponse, error)
}

type KatagoUseCase struct {
	store KatagoStore
	katagoRPC.UnimplementedKatagoServiceServer
}

func NewKatagoUseCase(store KatagoStore) *KatagoUseCase {
	return &KatagoUseCase{
		store: store,
	}
}

func (k *KatagoUseCase) GenerateMove(ctx context.Context, in *katagoRPC.Moves) (*katagoRPC.BotResponse, error) {
	// Преобразуем RPC-структуру в доменную модель
	moves := ConvertRPCMovesToDomain(*in)
	movesStrings := extractCoordinates(moves)

	// Вызов логики генерации хода через store
	botResponseDomain, err := k.store.GenerateMove(ctx, movesStrings)
	if err != nil {
		return nil, err
	}

	// Преобразуем доменный ответ в RPC-структуру
	resp := &katagoRPC.BotResponse{
		BotMove: botResponseDomain.BotMove,
		//RequestId: botResponseDomain.RequestId,
		// Если у вас есть дополнительные поля, их тоже нужно преобразовать
	}
	return resp, nil
}

func extractCoordinates(moves game.Moves) []string {
	coords := make([]string, 0)
	for _, m := range moves.Moves {
		coords = append(coords, m.Coordinates)
	}
	return coords
}

func ConvertRPCMovesToDomain(movesOld katagoRPC.Moves) game.Moves {
	domainMoves := make([]game.Move, 0)
	for _, m := range movesOld.Moves {
		move := game.Move{
			Coordinates: m.Coordinates,
			Color:       m.Color,
		}
		domainMoves = append(domainMoves, move)
	}
	return game.Moves{Moves: domainMoves}
}
