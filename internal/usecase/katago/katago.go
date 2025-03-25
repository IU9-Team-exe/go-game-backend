package katago

import (
	"context"
	"team_exe/internal/domain"
	katagoRPC "team_exe/microservices/proto"
)

func GenMove(ctx context.Context, moves domain.Moves, katagoGRPC katagoRPC.KatagoServiceClient) (domain.Move, error) {
	movesRPC := ConvertDomainMovesToRPC(moves)

	botResponse, err := katagoGRPC.GenerateMove(ctx, &movesRPC)
	if err != nil {
		return domain.Move{}, err
	}

	return domain.Move{
		Coordinates: botResponse.BotMove,
		Color:       "w",
	}, nil
}

func ConvertDomainMovesToRPC(movesDomain domain.Moves) katagoRPC.Moves {
	rpcMoves := make([]*katagoRPC.Move, 0)
	for _, m := range movesDomain.Moves {
		move := &katagoRPC.Move{
			Coordinates: m.Coordinates,
			Color:       m.Color,
		}
		rpcMoves = append(rpcMoves, move)
	}
	return katagoRPC.Moves{Moves: rpcMoves}
}
