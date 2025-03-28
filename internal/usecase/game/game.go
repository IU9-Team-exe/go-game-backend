package game

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"team_exe/internal/domain/game"
	sgf "team_exe/internal/domain/sgf"
	"team_exe/internal/errors"
	"team_exe/internal/statuses"
	"time"
)

type GameStore interface {
	GenerateGameKeys(ctx context.Context) (gameKeySecret string, gameKeyPublic string)
	PutGameToMongoDatabase(ctx context.Context, gameData game.Game) bool
	AddPlayer(ctx context.Context, userId string, gameKey string) (game.Game, bool)
	GetGameByGameKey(ctx context.Context, gameKey string) game.Game
	SaveSGFToRedis(key string, sgfText string) error
	LoadSGFFromRedis(key string) (string, error)
	HasUserActiveGameByUserId(ctx context.Context, userID string) (bool, error)
	GetGameByPublicKey(ctx context.Context, gameKeyPublic string) (game.Game, error)
	GetUserByID(ctx context.Context, userID string) game.GameUser
}

type GameUseCase struct {
	store GameStore
}

func NewGameUseCase(store GameStore) *GameUseCase {
	return &GameUseCase{store: store}
}

func (g *GameUseCase) CreateGame(ctx context.Context, newGameRequest game.CreateGameRequest, creatorID string) (err error, gameKeyPublic string, gameKeySecret string) {
	gameKeySecret, gameKeyPublic = g.store.GenerateGameKeys(ctx)

	newGame := game.Game{
		BoardSize:     newGameRequest.BoardSize,
		Komi:          newGameRequest.Komi,
		GameKeySecret: gameKeySecret,
		GameKeyPublic: gameKeyPublic,
		Status:        statuses.StatusWaitOpponent,
		CreatedAt:     time.Now(),
	}

	if newGameRequest.IsCreatorBlack {
		newGame.PlayerBlack = creatorID
	} else {
		newGame.PlayerWhite = creatorID
	}

	// getUserById - TODO добавить его в срез Users

	ok := g.store.PutGameToMongoDatabase(ctx, newGame)
	if !ok {
		return errors.ErrCreateGameFailed, "", ""
	}
	return nil, gameKeyPublic, gameKeySecret
}

func (g *GameUseCase) JoinGame(ctx context.Context, play game.Game, userID string) (game game.Game, err error) {
	updatedGame, ok := g.store.AddPlayer(ctx, userID, play.GameKeySecret)
	if !ok {
		return game, errors.ErrCreateGameFailed
	}

	minSGF := g.PrepareSgfFile(updatedGame)
	sgfString := SerializeSGF(&minSGF)
	err = g.store.SaveSGFToRedis(updatedGame.GameKeySecret, sgfString)
	if err != nil {
		return game, err
	}

	return updatedGame, nil
}

func (g *GameUseCase) GetGameByPublicKey(ctx context.Context, gameKeyPublic string) (game.Game, error) {
	play, err := g.store.GetGameByPublicKey(ctx, gameKeyPublic)
	if err != nil {
		return game.Game{}, err
	}

	if play.GameKeySecret == "" {
		return game.Game{}, fmt.Errorf("игры с ключом %s не найдено", gameKeyPublic)
	}

	return play, nil
}

func (g *GameUseCase) GetGameByID(ctx context.Context, gameUniqueKey string) (game.Game, error) {
	gameFromDb := g.store.GetGameByGameKey(ctx, gameUniqueKey)
	if gameFromDb.GameKeySecret == "" {
		return game.Game{}, errors.ErrGameNotFound
	}
	return g.store.GetGameByGameKey(ctx, gameUniqueKey), nil
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

func (g *GameUseCase) IsUserInGameByGameId(ctx context.Context, userID string, gameKey string) bool {
	play := g.store.GetGameByGameKey(ctx, gameKey)
	if play.PlayerWhite == userID || play.PlayerBlack == userID {
		return true
	}
	return false
}

func (g *GameUseCase) HasUserActiveGamesByUserId(ctx context.Context, userID string) (bool, error) {
	isAlreadyInGame, err := g.store.HasUserActiveGameByUserId(ctx, userID)
	if err != nil {
		return true, err
	}
	return isAlreadyInGame, nil
}
