package game

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"team_exe/internal/domain/game"
	sgf "team_exe/internal/domain/sgf"
	"team_exe/internal/domain/user"
	errs "team_exe/internal/errors"
	"team_exe/internal/statuses"
	"team_exe/internal/usecase/auth"
	"team_exe/internal/usecase/katago"
)

type GameStore interface {
	GenerateGameKeys(ctx context.Context) (secret, public string, err error)
	InsertGame(ctx context.Context, gameData game.Game) error
	AddPlayer(ctx context.Context, updatedGame *game.Game) error
	GetGameByGameKey(ctx context.Context, key string) (*game.Game, error)

	SaveSGFToRedis(ctx context.Context, key, sgfText string) error
	SaveSGFToMongo(ctx context.Context, key, sgfText string) error
	SaveMovesToMongo(ctx context.Context, key string, moves []game.Move) error
	LoadSGFFromRedis(ctx context.Context, key string) (string, error)

	HasUserActiveGameByUserId(ctx context.Context, userID string) ([]game.Game, error)
	GetGameByPublicKey(ctx context.Context, public string) (*game.Game, error)
	GetActiveGameByUserId(ctx context.Context, userID string) (game.Game, error)

	LeaveGameBySecretKey(ctx context.Context, secretKey, userID string) error
	CompleteGame(ctx context.Context, secretKey, finalSgf string) error

	ParseSGF(sgfText string) (*game.Game, error)

	GetArchiveGamesByYear(ctx context.Context, year, pageNum int) (*game.ArchiveResponse, error)
	GetArchiveYears(ctx context.Context) (*game.ArchiveYearsResponse, error)
	GetArchiveGamesByName(ctx context.Context, name string, pageNum int) (*game.ArchiveResponse, error)
	GetArchiveNames(ctx context.Context, pageNum int) (*game.ArchiveNamesResponse, error)
	GetGameFromArchiveById(ctx context.Context, id string) (*game.GameFromArchive, error)

	DetermineFreeColor(play *game.Game) (string, error)
}

type GameUseCase struct {
	store         GameStore
	katagoUsecase *katago.KatagoUseCase
	userUsecase   *auth.UserUsecaseHandler
}

func NewGameUseCase(store GameStore, authUC *auth.UserUsecaseHandler, kata *katago.KatagoUseCase) *GameUseCase {
	return &GameUseCase{
		store:         store,
		userUsecase:   authUC,
		katagoUsecase: kata,
	}
}

// -----------------------------------------------------------------------------
//  GAME CREATION
// -----------------------------------------------------------------------------

func (g *GameUseCase) CreateGame(ctx context.Context, req game.CreateGameRequest, creatorID string) (*game.Game, error) {
	// forbid multiple simultaneous human-games
	active, err := g.HasUserActiveGamesByUserId(ctx, creatorID)
	if err != nil {
		return nil, err
	}
	for _, ex := range active {
		if !isBotGame(&ex) {
			return &ex, errs.ErrUserAlreadyInGame
		}
	}

	secret, public, err := g.store.GenerateGameKeys(ctx)
	if err != nil {
		return nil, err
	}

	newGame := &game.Game{
		BoardSize:     req.BoardSize,
		Komi:          req.Komi,
		GameKeySecret: secret,
		GameKeyPublic: public,
		Status:        statuses.StatusWaitOpponent,
		CreatedAt:     time.Now(),
		Rules:         req.Rules,
	}

	userColor := "white"
	if req.IsCreatorBlack {
		newGame.PlayerBlack = creatorID
		userColor = "black"
	} else {
		newGame.PlayerWhite = creatorID
	}

	u, err := g.userUsecase.GetUserByUserId(ctx, creatorID)
	if err != nil {
		return nil, err
	}
	gameUser := ConvertUserToGameUser(u)
	gameUser.Role = "player"
	gameUser.Color = userColor
	newGame.Users = append(newGame.Users, gameUser)

	if err = g.store.InsertGame(ctx, *newGame); err != nil {
		return nil, errs.ErrCreateGameFailed
	}
	return newGame, nil
}

// -----------------------------------------------------------------------------
//  JOIN GAME
// -----------------------------------------------------------------------------

func (g *GameUseCase) JoinGame(ctx context.Context, publicKey, role, userID string) (*game.Game, error) {
	active, err := g.HasUserActiveGamesByUserId(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, ex := range active {
		if !isBotGame(&ex) {
			return &ex, errs.ErrUserAlreadyInGame
		}
	}

	play, err := g.store.GetGameByPublicKey(ctx, publicKey)
	if err != nil {
		return nil, err
	}
	if play.GameKeySecret == "" {
		return nil, errs.ErrGameNotFound
	}

	u, err := g.userUsecase.GetUserByUserId(ctx, userID)
	if err != nil {
		return nil, err
	}
	gameUser := ConvertUserToGameUser(u)
	gameUser.Role = role

	color, err := g.store.DetermineFreeColor(play)
	if err != nil {
		return nil, err
	}
	gameUser.Color = color
	if color == "black" {
		play.PlayerBlack = userID
	} else {
		play.PlayerWhite = userID
	}
	play.Users = append(play.Users, gameUser)
	now := time.Now()
	play.StartedAt = &now
	play.Status = statuses.StatusInProgress

	if err = g.store.AddPlayer(ctx, play); err != nil {
		return nil, err
	}

	sgfDoc := g.PrepareSgfFile(*play)
	if err = g.store.SaveSGFToRedis(ctx, play.GameKeySecret, SerializeSGF(&sgfDoc)); err != nil {
		return nil, err
	}

	return play, nil
}

// -----------------------------------------------------------------------------
//  LEAVE GAME
// -----------------------------------------------------------------------------

func (g *GameUseCase) LeaveGame(ctx context.Context, userID, key string) (bool, error) {
	var secret string
	var play *game.Game
	var err error

	if isKeyPublic(key) {
		play, err = g.store.GetGameByPublicKey(ctx, key)
		if err != nil {
			return false, err
		}
		secret = play.GameKeySecret
	} else {
		secret = key
		play, err = g.store.GetGameByGameKey(ctx, secret)
		if err != nil {
			return false, err
		}
	}

	// only one real player -> simple leave
	if (play.PlayerWhite == "" && play.PlayerBlack != "") || (play.PlayerWhite != "" && play.PlayerBlack == "") {
		if err = g.store.LeaveGameBySecretKey(ctx, secret, userID); err != nil {
			return false, err
		}
		return true, nil
	}

	// both players existed -> count lose to leaver
	if play.PlayerWhite != "" && play.PlayerBlack != "" {
		if err = g.userUsecase.AddLose(userID); err != nil {
			return false, err
		}
		if err = g.store.LeaveGameBySecretKey(ctx, secret, userID); err != nil {
			return false, err
		}
	}
	return true, nil
}

func isKeyPublic(key string) bool { return len(key) == 5 }

// -----------------------------------------------------------------------------
//  GAME INFO
// -----------------------------------------------------------------------------

func (g *GameUseCase) GetGameByPublicKey(ctx context.Context, public string) (*game.Game, error) {
	play, err := g.store.GetGameByPublicKey(ctx, public)
	if err != nil {
		return nil, err
	}
	if play.GameKeySecret == "" {
		return nil, fmt.Errorf("игры с ключом %s не найдено", public)
	}
	sgfStr, _ := g.GetSgfStringByGameKey(ctx, play.GameKeySecret)
	play.Sgf = sgfStr
	return play, nil
}

func (g *GameUseCase) GetGameInfoByPublicKey(ctx context.Context, public string) (*game.Game, error) {
	play, err := g.store.GetGameByPublicKey(ctx, public)
	if err != nil {
		return nil, err
	}
	if play.GameKeySecret == "" {
		return nil, fmt.Errorf("игры с ключом %s не найдено", public)
	}
	sgfStr, _ := g.GetSgfStringByGameKey(ctx, play.GameKeySecret)
	play.Sgf = sgfStr
	return play, nil
}

func (g *GameUseCase) GetGameBySecreteKey(ctx context.Context, secret string) (game.Game, error) {
	play, err := g.store.GetGameByGameKey(ctx, secret)
	if err != nil {
		return game.Game{}, err
	}
	if play.GameKeySecret == "" {
		return game.Game{}, errs.ErrGameNotFound
	}
	return *play, nil
}

// -----------------------------------------------------------------------------
//  SGF HELPERS
// -----------------------------------------------------------------------------

func (g *GameUseCase) PrepareSgfFile(data game.Game) sgf.SGF {
	return sgf.SGF{
		Root: &sgf.GameTree{
			Nodes: []sgf.Node{{
				Properties: map[string][]string{
					"FF": {"4"},
					"GM": {"1"},
					"SZ": {strconv.Itoa(data.BoardSize)},
					"PB": {data.PlayerBlack},
					"PW": {data.PlayerWhite},
					"DT": {data.CreatedAt.Format(time.RFC3339)},
					"KM": {strconv.FormatFloat(data.Komi, 'f', 1, 64)},
					"RU": {"Chinese"},
					"C":  {fmt.Sprintf("id:%s", data.GameKeySecret)},
				},
			}},
		},
	}
}

func AddMovesToSgf(tree *sgf.GameTree, moves []game.Move) {
	for _, mv := range moves {
		tree.Nodes = append(tree.Nodes, sgf.Node{
			Properties: map[string][]string{
				mv.Color: {mv.Coordinates},
			},
		})
	}
}

func (g *GameUseCase) GetSgfStringByGameKey(ctx context.Context, key string) (string, error) {
	return g.store.LoadSGFFromRedis(ctx, key)
}

func SerializeSGF(s *sgf.SGF) string {
	var b strings.Builder
	b.WriteString("(")
	serializeGameTree(&b, s.Root)
	b.WriteString(")")
	return b.String()
}

func serializeGameTree(b *strings.Builder, tree *sgf.GameTree) {
	for _, n := range tree.Nodes {
		b.WriteString(";")

		order := []string{"FF", "GM", "SZ", "PB", "PW", "DT", "RE", "KM", "RU", "C", "B", "W"}
		seen := map[string]bool{}
		for _, k := range order {
			if vals, ok := n.Properties[k]; ok {
				seen[k] = true
				for _, v := range vals {
					fmt.Fprintf(b, "%s[%s]", k, v)
				}
			}
		}
		for k, vals := range n.Properties {
			if seen[k] {
				continue
			}
			for _, v := range vals {
				fmt.Fprintf(b, "%s[%s]", k, v)
			}
		}
	}
	for _, ch := range tree.Children {
		b.WriteString("(")
		serializeGameTree(b, ch)
		b.WriteString(")")
	}
}

// -----------------------------------------------------------------------------
//  ADD MOVE (human)
// -----------------------------------------------------------------------------

func (g *GameUseCase) AddMoveToGameSgf(ctx context.Context, key string, mv game.Move) (*game.MoveInfoWS, error) {
	old, _ := g.GetSgfStringByGameKey(ctx, key)
	play, _ := g.store.ParseSGF(old)

	if len(play.Moves) == 0 {
		now := time.Now()
		play.StartedAt = &now
		play.Status = statuses.StatusInProgress
	}

	kataMoves := append(play.Moves, mv)
	kataResp, err := g.katagoUsecase.CheckMove(ctx, play.GameKeySecret, &game.Moves{Moves: kataMoves}, play.BoardSize, play.Rules)

	resp := &game.MoveInfoWS{NewSgf: old}
	if err != nil || kataResp.Error != "" {
		resp.IsMoveCorrect = false
		if kataResp != nil {
			resp.Error = kataResp.Error
		} else {
			resp.Error = "kataResp is nil"
		}
		return resp, nil
	}

	raw := kataToSgf(mv.Coordinates, play.BoardSize)
	newSgf := fmt.Sprintf("%s;%s[%s])", strings.TrimSuffix(old, ")"), mv.Color, raw)
	resp.IsMoveCorrect = true
	resp.NewSgf = newSgf

	if err = g.store.SaveSGFToRedis(ctx, key, newSgf); err != nil {
		return nil, err
	}

	all := append(play.Moves, mv)
	if len(all) >= 2 &&
		all[len(all)-1].Coordinates == "pass" &&
		all[len(all)-2].Coordinates == "pass" {
		resp.IsGameFinished = true
		if err = g.store.CompleteGame(ctx, key, newSgf); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

// -----------------------------------------------------------------------------
//  KATAGO ANALYSE
// -----------------------------------------------------------------------------

func (g *GameUseCase) AnalyseCurrentGame(ctx context.Context, secret string) (*game.KataGoResponse, error) {
	play, err := g.GetGameBySecreteKey(ctx, secret)
	if err != nil {
		return nil, err
	}

	var sgfText string
	if !play.IsFromArchive {
		sgfText, err = g.store.LoadSGFFromRedis(ctx, secret)
		if err != nil {
			return nil, err
		}
	} else {
		sgfText = play.Sgf
	}

	parsed, err := g.store.ParseSGF(sgfText)
	if err != nil {
		return nil, err
	}

	return g.katagoUsecase.AnalyseCurrentGame(
		ctx,
		parsed.GameKeySecret,
		&game.Moves{Moves: parsed.Moves},
		parsed.BoardSize,
		parsed.Rules,
	)
}

// -----------------------------------------------------------------------------
//  BOT PLAY
// -----------------------------------------------------------------------------

func (g *GameUseCase) GenerateMoveAgainstBot(ctx context.Context, secret string, userMove game.Move) ([]game.Move, string, error) {
	old, err := g.store.LoadSGFFromRedis(ctx, secret)
	if err != nil {
		return nil, "", err
	}

	play, err := g.store.ParseSGF(old)
	if err != nil {
		return nil, "", err
	}

	userBotMoves := append(play.Moves, userMove)
	botInfo, err := g.katagoUsecase.GenerateMove(ctx, secret, &game.Moves{Moves: userBotMoves}, play.BoardSize, play.Rules)
	if err != nil {
		return nil, "", err
	}

	botMove := game.Move{Color: oppositeColor(userMove.Color), Coordinates: botInfo.Move}
	allMoves := append(userBotMoves, botMove)

	base := strings.TrimSuffix(old, ")")
	rawUser := kataToSgf(userMove.Coordinates, play.BoardSize)
	base = fmt.Sprintf("%s;%s[%s]", base, userMove.Color, rawUser)
	rawBot := kataToSgf(botMove.Coordinates, play.BoardSize)
	newSgf := fmt.Sprintf("%s;%s[%s])", base, botMove.Color, rawBot)

	if err = g.store.SaveSGFToRedis(ctx, secret, newSgf); err != nil {
		return nil, "", err
	}
	if err = g.store.SaveSGFToMongo(ctx, secret, newSgf); err != nil {
		return nil, "", err
	}
	if err = g.store.SaveMovesToMongo(ctx, secret, allMoves); err != nil {
		return nil, "", err
	}

	return allMoves, newSgf, nil
}

// -----------------------------------------------------------------------------
//  CREATE BOT GAME
// -----------------------------------------------------------------------------

func (g *GameUseCase) CreateBotGame(ctx context.Context, req game.CreateGameRequest, creatorID string) (*game.Game, error) {
	active, err := g.HasUserActiveGamesByUserId(ctx, creatorID)
	if err != nil {
		return nil, err
	}
	for _, ex := range active {
		if isBotGame(&ex) {
			return &ex, errs.ErrUserAlreadyInGame
		}
	}

	secret, public, err := g.store.GenerateGameKeys(ctx)
	if err != nil {
		return nil, err
	}

	botGame := &game.Game{
		BoardSize:     req.BoardSize,
		Komi:          req.Komi,
		GameKeySecret: secret,
		GameKeyPublic: public,
		Status:        statuses.StatusInProgress,
		CreatedAt:     time.Now(),
		Rules:         req.Rules,
	}

	u, err := g.userUsecase.GetUserByUserId(ctx, creatorID)
	if err != nil {
		return nil, err
	}
	player := ConvertUserToGameUser(u)
	player.Role = "player"

	if req.IsCreatorBlack {
		botGame.PlayerBlack = creatorID
		player.Color = "black"
	} else {
		botGame.PlayerWhite = creatorID
		player.Color = "white"
	}

	botUser := &game.GameUser{
		ID:       "bot",
		Username: "bot",
		Color:    oppositeColor(player.Color),
		Role:     "bot",
		Rating:   0,
	}
	if player.Color == "black" {
		botGame.PlayerWhite = "bot"
	} else {
		botGame.PlayerBlack = "bot"
	}

	botGame.Users = []*game.GameUser{player, botUser}

	if err = g.store.InsertGame(ctx, *botGame); err != nil {
		return nil, errs.ErrCreateGameFailed
	}

	preparedSgf := g.PrepareSgfFile(*botGame)
	sgfStr := SerializeSGF(&preparedSgf)
	if err = g.store.SaveSGFToRedis(ctx, secret, sgfStr); err != nil {
		return nil, err
	}
	if err = g.store.SaveSGFToMongo(ctx, secret, sgfStr); err != nil {
		return nil, err
	}

	return botGame, nil
}

// -----------------------------------------------------------------------------
//  ARCHIVE
// -----------------------------------------------------------------------------

func (g *GameUseCase) GetArchiveOfGames(ctx context.Context, page, year int, name string) (*game.ArchiveResponse, error) {
	if year != 0 {
		return g.store.GetArchiveGamesByYear(ctx, year, page)
	}
	if name != "" {
		return g.store.GetArchiveGamesByName(ctx, name, page)
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

func (g *GameUseCase) GetListOfArchiveNames(ctx context.Context, page int) (*game.ArchiveNamesResponse, error) {
	return g.store.GetArchiveNames(ctx, page)
}

func (g *GameUseCase) GetGameFromArchiveById(ctx context.Context, id string) (*game.GameFromArchive, error) {
	return g.store.GetGameFromArchiveById(ctx, id)
}

// -----------------------------------------------------------------------------
//  UTILS
// -----------------------------------------------------------------------------

func ConvertUserToGameUser(u user.User) *game.GameUser {
	return &game.GameUser{
		ID:       u.ID,
		Username: u.Username,
		Rating:   float64(u.Rating),
	}
}

func isBotGame(gm *game.Game) bool {
	return gm.PlayerBlack == "bot" || gm.PlayerWhite == "bot"
}

func kataToSgf(kata string, size int) string {
	if kata == "pass" {
		return ""
	}
	col := kata[0]
	row, _ := strconv.Atoi(kata[1:])
	var x int
	if col < 'I' {
		x = int(col - 'A')
	} else {
		x = int(col - 'A' - 1)
	}
	y := size - row
	return fmt.Sprintf("%c%c", 'a'+x, 'a'+y)
}

func oppositeColor(c string) string {
	if c == "B" || c == "black" {
		return "W"
	}
	return "B"
}

// -----------------------------------------------------------------------------
//  MISC
// -----------------------------------------------------------------------------

func (g *GameUseCase) IsUserInGameByGameId(ctx context.Context, userID, key string) bool {
	play, err := g.store.GetGameByGameKey(ctx, key)
	if err != nil {
		return false
	}
	return play.PlayerWhite == userID || play.PlayerBlack == userID
}

func (g *GameUseCase) HasUserActiveGamesByUserId(ctx context.Context, userID string) ([]game.Game, error) {
	return g.store.HasUserActiveGameByUserId(ctx, userID)
}

func (g *GameUseCase) GetActiveGameSecretKey(ctx context.Context, userID string, withBot bool) (string, error) {
	games, err := g.store.HasUserActiveGameByUserId(ctx, userID)
	if err != nil {
		return "", err
	}
	for _, p := range games {
		if isBotGame(&p) == withBot {
			return p.GameKeySecret, nil
		}
	}
	return "", fmt.Errorf("no active game for user %s (bot=%v)", userID, withBot)
}
