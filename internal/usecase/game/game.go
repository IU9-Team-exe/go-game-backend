package game

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"team_exe/internal/domain/game"
	sgf "team_exe/internal/domain/sgf"
	"team_exe/internal/errors"
)

type GameStore interface {
	GenerateGameKey(ctx context.Context) string
	PutGameToMongoDatabase(ctx context.Context, gameData game.Game) bool
	AddPlayer(ctx context.Context, newUser game.GameUser, gameKey string) bool
	ConvertToUserFromJoinReq(ctx context.Context, joinRequest game.GameJoinRequest) game.GameUser
	GeyGameByGameKey(ctx context.Context, gameKey string) game.Game
	SaveSGFToRedis(key string, sgfText string) error
	LoadSGFFromRedis(key string) (string, error)
}

type GameUseCase struct {
	store GameStore
}

func NewGameUseCase(store GameStore) *GameUseCase {
	return &GameUseCase{store: store}
}

func (g *GameUseCase) CreateGame(ctx context.Context, gameData game.Game) (err error, gameUniqueKey string) {
	gameUniqueKey = g.store.GenerateGameKey(ctx)
	gameData.GameKey = gameUniqueKey

	ok := g.store.PutGameToMongoDatabase(ctx, gameData)
	if !ok {
		return errors.ErrCreateGameFailed, ""
	}
	return nil, gameUniqueKey
}

func (g *GameUseCase) JoinGame(ctx context.Context, gameJoinData game.GameJoinRequest) (err error) {
	newUser := g.store.ConvertToUserFromJoinReq(ctx, gameJoinData)
	ok := g.store.AddPlayer(ctx, newUser, gameJoinData.GameKey)
	if !ok {
		return errors.ErrCreateGameFailed
	}

	foundGame, err := g.GetGameByID(ctx, gameJoinData.GameKey)
	if err != nil {
		return err
	}

	minSGF := g.PrepareSgfFile(foundGame)
	sgfString := SerializeSGF(&minSGF)
	err = g.store.SaveSGFToRedis(foundGame.GameKey, sgfString)
	if err != nil {
		return err
	}

	return nil
}

func (g *GameUseCase) GetGameByID(ctx context.Context, gameUniqueKey string) (game.Game, error) {
	gameFromDb := g.store.GeyGameByGameKey(ctx, gameUniqueKey)
	if gameFromDb.GameKey == "" {
		return game.Game{}, errors.ErrGameNotFound
	}
	return g.store.GeyGameByGameKey(ctx, gameUniqueKey), nil
}

func (g *GameUseCase) PrepareSgfFile(gameData game.Game) sgf.SGF {
	minSGF := sgf.SGF{
		Root: &sgf.GameTree{
			Nodes: []sgf.Node{
				{
					Properties: map[string][]string{
						"FF": {"4"},
						"GM": {"1"},
						"SZ": {strconv.Itoa(gameData.BoardSize)},
						"PB": {gameData.PlayerBlack},
						"PW": {gameData.PlayerWhite},
						"DT": {gameData.CreatedAt.String()},
						"RE": {""},
						"KM": {strconv.FormatFloat(gameData.Komi, 'f', 1, 64)},
						"RU": {"Chinese"},
						"C":  {"Game 1 x 1"},
					},
				},
			},
		},
	}
	return minSGF
}

func AddMovesToSgf(tree *sgf.GameTree, moves []game.Move) {
	for _, move := range moves {
		node := sgf.Node{
			Properties: map[string][]string{
				move.Color: {move.Coordinates},
			},
		}
		tree.Nodes = append(tree.Nodes, node)
	}
}

func (g *GameUseCase) GetSgfStringByGameKey(key string) (string, error) {
	return g.store.LoadSGFFromRedis(key)
}

func SerializeSGF(s *sgf.SGF) string {
	var builder strings.Builder
	builder.WriteString("(")
	serializeGameTree(&builder, s.Root)
	builder.WriteString(")")
	return builder.String()
}

func serializeGameTree(builder *strings.Builder, tree *sgf.GameTree) {
	for _, node := range tree.Nodes {
		builder.WriteString(";")

		// фиксированный порядок свойств SGF
		orderedKeys := []string{"FF", "GM", "SZ", "PB", "PW", "DT", "RE", "KM", "RU", "C", "B", "W"}
		used := make(map[string]bool)
		for _, key := range orderedKeys {
			if values, ok := node.Properties[key]; ok {
				used[key] = true
				for _, v := range values {
					builder.WriteString(fmt.Sprintf("%s[%s]", key, v))
				}
			}
		}

		for key, values := range node.Properties {
			if !used[key] {
				for _, v := range values {
					builder.WriteString(fmt.Sprintf("%s[%s]", key, v))
				}
			}
		}
	}

	for _, child := range tree.Children {
		builder.WriteString("(")
		serializeGameTree(builder, child)
		builder.WriteString(")")
	}
}

func (g *GameUseCase) AddMoveToGameSgf(key string, move game.Move) (string, error) {
	sgfString, err := g.GetSgfStringByGameKey(key)
	if err != nil {
		return "", err
	}
	newSgfString := AppendMoveToSgf(sgfString, move)
	err = g.store.SaveSGFToRedis(key, newSgfString)
	if err != nil {
		return "", err
	}
	return newSgfString, nil
}

func AppendMoveToSgf(sgfText string, move game.Move) string {
	if strings.HasSuffix(sgfText, ")") {
		sgfText = sgfText[:len(sgfText)-1]
	}
	return sgfText + fmt.Sprintf(";%s[%s])", move.Color, move.Coordinates)
}
