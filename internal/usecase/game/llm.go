package game

import (
	"context"
	"fmt"
	"strings"
)

type LlmStore interface {
	SendRequestToLlm(request string) (response string, err error)
}

func sgfToStandard(sgfCoord string, boardSize int) (string, error) {
	if len(sgfCoord) != 2 {
		return "", fmt.Errorf("неверный формат SGF-координаты: %s", sgfCoord)
	}
	col := sgfCoord[0]
	row := sgfCoord[1]
	if col < 'a' || col > 's' || row < 'a' || row > 's' {
		return "", fmt.Errorf("координата выходит за пределы доски: %s", sgfCoord)
	}
	standardCol := string('A' + (col - 'a'))
	if standardCol >= "I" {
		standardCol = string(standardCol[0] + 1)
	}
	standardRow := boardSize - int(row-'a')
	return fmt.Sprintf("%s%d", standardCol, standardRow), nil
}

func (g *GameUseCase) ExplainMove(ctx context.Context, gameID string, moveSeqNumber int) (string, error) {
	foundGame, err := g.store.GetGameFromArchiveById(ctx, gameID)
	if err != nil {
		return "", err
	}
	if moveSeqNumber < 3 || moveSeqNumber >= len(foundGame.Moves)-3 {
		return "", fmt.Errorf("Нельзя разобрать три первых и последних хода")
	}
	var sB strings.Builder
	for i := 0; i < len(foundGame.Moves) && i < moveSeqNumber; i++ {
		currentMoveCoords, err := sgfToStandard(foundGame.Moves[i].Coordinates, 19)
		if err != nil {
			return "", err
		}
		sB.WriteString(currentMoveCoords)
		sB.WriteString(" ")
	}
	prevMoves := sB.String()
	currentMove, err := sgfToStandard(foundGame.Moves[moveSeqNumber].Coordinates, 19)
	if err != nil {
		return "", err
	}
	sB.Reset()
	for i := moveSeqNumber + 1; i < len(foundGame.Moves) && i < moveSeqNumber+3; i++ {
		currentMoveCoords, err := sgfToStandard(foundGame.Moves[i].Coordinates, 19)
		if err != nil {
			return "", err
		}
		sB.WriteString(currentMoveCoords)
		sB.WriteString(" ")
	}
	nextMoves := sB.String()
	req := fmt.Sprintf("Sequence of moves: %s\nCurrent move: %s\nNext moves: %s\n", prevMoves, currentMove, nextMoves, "")
	resp, err := g.llm.SendRequestToLlm(req)
	return resp, nil
}
