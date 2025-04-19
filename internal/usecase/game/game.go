package game

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"team_exe/internal/domain/game"
	sgf "team_exe/internal/domain/sgf"
	"team_exe/internal/domain/user"
	"team_exe/internal/errors"
	"team_exe/internal/statuses"
	"team_exe/internal/usecase/auth"
	"team_exe/internal/usecase/katago"
	"time"
)

type GameStore interface {
	GenerateGameKeys(ctx context.Context) (gameKeySecret string, gameKeyPublic string)
	PutGameToMongoDatabase(ctx context.Context, gameData game.Game) bool
	AddPlayer(ctx context.Context, updatedGame *game.Game) error
	GetGameByGameKey(ctx context.Context, gameKey string) game.Game
	SaveSGFToRedis(key string, sgfText string) error
	LoadSGFFromRedis(key string) (string, error)
	HasUserActiveGameByUserId(ctx context.Context, userID string) (bool, *game.Game, error)
	GetGameByPublicKey(ctx context.Context, gameKeyPublic string) (*game.Game, error)
	GetActiveGameByUserId(ctx context.Context, userID string) (game.Game, error)

	LeaveGameBySecretKey(ctx context.Context, secretKey string, userID string) error
	CompleteGame(ctx context.Context, secretKey, finalSgf string) error

	ParseSGF(sgfText string) (*game.Game, error)

	GetArchiveGamesByYear(ctx context.Context, year int, pageNum int) (*game.ArchiveResponse, error)
	GetArchiveYears(ctx context.Context) (*game.ArchiveYearsResponse, error)
	GetArchiveGamesByName(ctx context.Context, name string, pageNum int) (*game.ArchiveResponse, error)
	GetArchiveNames(ctx context.Context, pageNum int) (*game.ArchiveNamesResponse, error)
	GetGameFromArchiveById(ctx context.Context, gameFromArchiveById string) (*game.GameFromArchive, error)

	DetermineFreeColor(play *game.Game) (string, error)
}

type GameUseCase struct {
	store         GameStore
	katagoUsecase *katago.KatagoUseCase
	userUsecase   *auth.UserUsecaseHandler
}

func NewGameUseCase(store GameStore, auth *auth.UserUsecaseHandler, katagoUC *katago.KatagoUseCase) *GameUseCase {
	return &GameUseCase{
		store:         store,
		userUsecase:   auth,
		katagoUsecase: katagoUC,
	}
}

func (g *GameUseCase) CreateGame(ctx context.Context, newGameRequest game.CreateGameRequest, creatorID string) (*game.Game, error) {
	gameKeySecret, gameKeyPublic := g.store.GenerateGameKeys(ctx)

	newGame := &game.Game{
		BoardSize:     newGameRequest.BoardSize,
		Komi:          newGameRequest.Komi,
		GameKeySecret: gameKeySecret,
		GameKeyPublic: gameKeyPublic,
		Status:        statuses.StatusWaitOpponent,
		CreatedAt:     time.Now(),
		Rules:         newGameRequest.Rules,
	}
	userPlayedColor := ""

	if newGameRequest.IsCreatorBlack {
		newGame.PlayerBlack = creatorID
		userPlayedColor = "black"
	} else {
		newGame.PlayerWhite = creatorID
		userPlayedColor = "white"
	}

	userById, err := g.userUsecase.GetUserByUserId(ctx, creatorID)
	if err != nil {
		return nil, err
	}

	isAlreadyInGame, foundGameId, err := g.HasUserActiveGamesByUserId(ctx, creatorID)
	if err != nil {
		return nil, err
	}
	if isAlreadyInGame {
		newGame.GameKeyPublic = foundGameId
		return newGame, errors.ErrUserAlreadyInGame
	}

	gameUser := ConvertUserToGameUser(userById)
	gameUser.Role = "player"
	gameUser.Color = userPlayedColor

	newGame.Users = append(newGame.Users, gameUser)

	ok := g.store.PutGameToMongoDatabase(ctx, *newGame)
	if !ok {
		return nil, errors.ErrCreateGameFailed
	}
	return newGame, nil
}

func ConvertUserToGameUser(user user.User) *game.GameUser {
	return &game.GameUser{
		ID:       user.ID,
		Username: user.Username,
		Rating:   float64(user.Rating),
	}
}

func (g *GameUseCase) JoinGame(ctx context.Context, gameKeyPublic string, userRole, userID string) (*game.Game, error) {
	inGame, foundGameId, err := g.HasUserActiveGamesByUserId(ctx, userID)
	if err != nil {
		return nil, err
	}

	if inGame {
		return &game.Game{GameKeyPublic: foundGameId}, errors.ErrUserAlreadyInGame
	}

	play, err := g.store.GetGameByPublicKey(ctx, gameKeyPublic)
	if err != nil {
		return nil, err
	}
	if play.GameKeySecret == "" {
		return nil, errors.ErrGameNotFound
	}

	userById, err := g.userUsecase.GetUserByUserId(ctx, userID)
	if err != nil {
		return nil, err
	}

	gameUser := ConvertUserToGameUser(userById)
	gameUser.Role = userRole
	calculatedColor, err := g.store.DetermineFreeColor(play)
	if err != nil {
		return nil, err
	}

	if calculatedColor == "black" {
		play.PlayerBlack = userID
	} else {
		play.PlayerWhite = userID
	}

	gameUser.Color = calculatedColor
	play.Users = append(play.Users, gameUser)

	now := time.Now()
	play.StartedAt = &now
	play.Status = statuses.StatusInProgress

	err = g.store.AddPlayer(ctx, play)
	if err != nil {
		return nil, err
	}

	minSGF := g.PrepareSgfFile(*play)
	sgfStr := SerializeSGF(&minSGF)
	if err = g.store.SaveSGFToRedis(play.GameKeySecret, sgfStr); err != nil {
		return nil, err
	}

	return play, nil
}

func (g *GameUseCase) LeaveGame(ctx context.Context, gamePublicKey, userID string) (bool, error) {
	play, err := g.store.GetActiveGameByUserId(ctx, userID)
	if err != nil {
		return false, err
	}
	if (play.PlayerWhite == "" && play.PlayerBlack != "") || (play.PlayerWhite != "" && play.PlayerBlack == "") {
		// пользователь один, значит просто выходит
		err = g.store.LeaveGameBySecretKey(ctx, play.GameKeySecret, userID)
		if err != nil {
			return false, err
		}

		return true, nil
	} else if play.PlayerWhite != "" && play.PlayerBlack != "" {
		err := g.userUsecase.AddLose(userID)
		if err != nil {
			return false, err
		}
		err = g.store.LeaveGameBySecretKey(ctx, play.GameKeySecret, userID)
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

func (g *GameUseCase) GetGameByPublicKey(ctx context.Context, gameKeyPublic string) (*game.Game, error) {
	play, err := g.store.GetGameByPublicKey(ctx, gameKeyPublic)
	if err != nil {
		return nil, err
	}

	if play.GameKeySecret == "" {
		return nil, fmt.Errorf("игры с ключом %s не найдено", gameKeyPublic)
	}

	sgfStringOfGame, err := g.GetSgfStringByGameKey(play.GameKeySecret)
	if err != nil {
		// а вот ничего не будет ахахах
	}

	play.Sgf = sgfStringOfGame

	return play, nil
}

func (g *GameUseCase) GetGameInfoByPublicKey(ctx context.Context, gameKeyPublic string) (*game.Game, error) {
	play, err := g.store.GetGameByPublicKey(ctx, gameKeyPublic)
	if err != nil {
		return nil, err
	}

	if play.GameKeySecret == "" {
		return nil, fmt.Errorf("игры с ключом %s не найдено", gameKeyPublic)
	}
	sgfStringOfGame, _ := g.GetSgfStringByGameKey(play.GameKeySecret)
	play.Sgf = sgfStringOfGame

	return play, nil
}

func (g *GameUseCase) GetGameBySecreteKey(ctx context.Context, gameUniqueKey string) (game.Game, error) {
	gameFromDb := g.store.GetGameByGameKey(ctx, gameUniqueKey)

	if gameFromDb.GameKeySecret == "" {
		return game.Game{}, errors.ErrGameNotFound
	}

	return gameFromDb, nil
}

func (g *GameUseCase) PrepareSgfFile(gameData game.Game) sgf.SGF {
	return sgf.SGF{
		Root: &sgf.GameTree{
			Nodes: []sgf.Node{{
				Properties: map[string][]string{
					"FF": {"4"},
					"GM": {"1"},
					"SZ": {strconv.Itoa(gameData.BoardSize)},
					"PB": {gameData.PlayerBlack},
					"PW": {gameData.PlayerWhite},
					"DT": {gameData.CreatedAt.Format(time.RFC3339)},
					"KM": {strconv.FormatFloat(gameData.Komi, 'f', 1, 64)},
					"RU": {"Chinese"},
					// Сразу заводим комментарий с ID
					"C": {fmt.Sprintf("id:%s", gameData.GameKeySecret)},
				},
			}},
		},
	}
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

func (g *GameUseCase) AddMoveToGameSgf(
	ctx context.Context,
	key string,
	move game.Move,
) (*game.MoveInfoWS, error) {
	oldSgf, _ := g.GetSgfStringByGameKey(key)
	play, _ := g.store.ParseSGF(oldSgf)

	if len(play.Moves) == 0 {
		currTime := time.Now()
		play.StartedAt = &currTime
		play.Status = statuses.StatusInProgress

	}

	kataMoves := append(play.Moves, move)

	kataResp, err := g.katagoUsecase.CheckMove(ctx, play.GameKeySecret, &game.Moves{Moves: kataMoves}, play.BoardSize, play.Rules)
	resp := &game.MoveInfoWS{NewSgf: oldSgf}
	if err != nil || kataResp.Error != "" {
		resp.IsMoveCorrect = false
		if kataResp != nil {
			resp.Error = kataResp.Error
		} else {
			resp.Error = "kataResp is nil!"
		}
		return resp, nil
	}

	raw := kataToSgf(move.Coordinates, play.BoardSize) // dd
	newSgf := fmt.Sprintf("%s;%s[%s])", strings.TrimSuffix(oldSgf, ")"), move.Color, raw)
	resp.IsMoveCorrect = true
	resp.NewSgf = newSgf
	err = g.store.SaveSGFToRedis(key, newSgf)
	if err != nil {
		return nil, err
	}

	all := append(play.Moves, move)
	n := len(all)
	if n >= 2 && all[n-1].Coordinates == "pass" && all[n-2].Coordinates == "pass" {
		resp.IsGameFinished = true
		err = g.store.CompleteGame(ctx, key, newSgf)
		if err != nil {
			return nil, err
		}
	}

	return resp, nil
}

func kataToSgf(kata string, boardSize int) string {
	if kata == "pass" {
		return ""
	}
	// буква
	col := kata[0]                   // 'A'..'T'
	row, _ := strconv.Atoi(kata[1:]) // 1..19
	// индекс по оси X (0..boardSize-1), пропуская 'I'
	var x int
	if col < 'I' {
		x = int(col - 'A')
	} else {
		x = int(col - 'A' - 1)
	}
	// индекс по оси Y: SGF 'a' — верхняя строка
	y := boardSize - row
	return fmt.Sprintf("%c%c", 'a'+x, 'a'+y)
}

func AppendMoveToSgf(sgfText string, move game.Move) string {
	if strings.HasSuffix(sgfText, ")") {
		sgfText = sgfText[:len(sgfText)-1]
	}
	return fmt.Sprintf("%s;%s[%s])", sgfText, move.Color, move.Coordinates)
}

func (g *GameUseCase) IsUserInGameByGameId(ctx context.Context, userID string, gameKey string) bool {
	play := g.store.GetGameByGameKey(ctx, gameKey)
	if play.PlayerWhite == userID || play.PlayerBlack == userID {
		return true
	}
	return false
}

func (g *GameUseCase) HasUserActiveGamesByUserId(ctx context.Context, userID string) (bool, string, error) {
	isAlreadyInGame, foundGame, err := g.store.HasUserActiveGameByUserId(ctx, userID)
	if err != nil {
		return true, "", err
	}

	if isAlreadyInGame {
		return true, foundGame.GameKeyPublic, nil
	}

	return false, "", nil
}

func (g *GameUseCase) GetArchiveOfGames(ctx context.Context, pageNumber, year int, name string) (*game.ArchiveResponse, error) {
	if year != 0 {
		archiveResp, err := g.store.GetArchiveGamesByYear(ctx, year, pageNumber)
		if err != nil {
			return nil, err
		}
		return archiveResp, nil
	}
	if name != "" {
		archiveResp, err := g.store.GetArchiveGamesByName(ctx, name, pageNumber)
		if err != nil {
			return nil, err
		}
		return archiveResp, nil
	}

	return nil, nil
}

func (g *GameUseCase) GetListOfArchiveYears(ctx context.Context) (*game.ArchiveYearsResponse, error) {
	resp, err := g.store.GetArchiveYears(ctx)
	if err == nil {
		resp.Years = resp.Years[2 : len(resp.Years)-2]
	}
	return resp, err
}

func (g *GameUseCase) GetListOfArchiveNames(ctx context.Context, pageNum int) (*game.ArchiveNamesResponse, error) {
	resp, err := g.store.GetArchiveNames(ctx, pageNum)
	if err == nil {
	}
	return resp, err
}

func (g *GameUseCase) GetGameFromArchiveById(ctx context.Context, gameFromArchiveById string) (*game.GameFromArchive, error) {
	foundGame, err := g.store.GetGameFromArchiveById(ctx, gameFromArchiveById)
	if err != nil {
		return nil, err
	}

	return foundGame, nil
}

func (g *GameUseCase) AnalyseCurrentGame(ctx context.Context, userID string, gameKeyPublic string) (*game.KataGoResponse, error) {
	mongoGame, err := g.GetGameByPublicKey(ctx, gameKeyPublic)
	if err != nil {
		return nil, err
	}

	sgfFromRedis, err := g.store.LoadSGFFromRedis(mongoGame.GameKeySecret)
	if err != nil {
		return nil, err
	}

	gameFromSgf, err := g.store.ParseSGF(sgfFromRedis)
	if err != nil {
		return nil, err
	}

	return g.katagoUsecase.AnalyseCurrentGame(ctx, gameFromSgf.GameKeySecret, &game.Moves{gameFromSgf.Moves}, gameFromSgf.BoardSize, gameFromSgf.Rules)
}
