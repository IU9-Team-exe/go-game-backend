package game

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"io"
	"log"
	"net/http"
	"sync"
	"team_exe/internal/adapters"
	"team_exe/internal/bootstrap"
	"team_exe/internal/delivery/auth"
	"team_exe/internal/domain/game"
	"team_exe/internal/httpresponse"
	repo "team_exe/internal/repository"
	gameuc "team_exe/internal/usecase/game"
	"team_exe/internal/utils"
)

type GameHandler struct {
	cfg          bootstrap.Config
	log          *zap.SugaredLogger
	gameUC       *gameuc.GameUseCase
	mongoAdapter *adapters.AdapterMongo
	redisAdapter *adapters.AdapterRedis
	authHandler  *auth.AuthHandler
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var activeGames = make(map[string]*game.Game)
var activeGamesMu sync.RWMutex

func NewGameHandler(cfg bootstrap.Config, log *zap.SugaredLogger, mongoAdapter *adapters.AdapterMongo, redisAdapter *adapters.AdapterRedis, authHandler *auth.AuthHandler) *GameHandler {
	return &GameHandler{
		cfg:         cfg,
		log:         log,
		gameUC:      gameuc.NewGameUseCase(repo.NewGameRepository(cfg, log, redisAdapter.GetClient(), mongoAdapter.Database)),
		authHandler: authHandler,
	}
}

func (g *GameHandler) GetGameById(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		g.log.Error("Failed to read body:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
	decoder.DisallowUnknownFields()

	var gameData game.GetGameInfoRequest
	if err = decoder.Decode(&gameData); err != nil {
		g.log.Error("JSON decode error:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}
	gameByID, err := g.gameUC.GetGameByID(r.Context(), gameData.GameKey)
	if err != nil {
		httpresponse.WriteResponseWithStatus(w, http.StatusInternalServerError,
			httpresponse.ErrorResponse{ErrorDescription: err.Error()})
		return
	}
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, gameByID)
}

func (g *GameHandler) HandleNewGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.log.Error("Only POST method is allowed")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	var newGameRequest game.CreateGameRequest
	if err := utils.DecodeJSONRequest(r, &newGameRequest); err != nil {
		g.log.Error("JSON decode error:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	if newGameRequest.BoardSize == 0 || newGameRequest.Komi == 0 {
		g.log.Error("запрос на создание игры не содержит размер поля или коми!")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "запрос на создание игры не содержит размер поля или коми! ")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		g.log.Error("Не нашли userID в куке")
		return
	}
	g.log.Infof("New game is from id: %s", userID)

	ctx := r.Context()
	isAlreadyInGame, err := g.gameUC.HasUserActiveGamesByUserId(ctx, userID)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "ошибка при проверке на вхождение в уже существующую в игру: "+err.Error())
		return
	}
	if isAlreadyInGame {
		g.log.Error("пользователь уже состоит в игре!")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "ошибка при добавлении в игру: игрок уже состоит в игре")
		return
	}

	err, gameKeyPublic, gameKeySecret := g.gameUC.CreateGame(ctx, newGameRequest, userID)
	if err != nil {
		g.log.Error(err)
		return
	}

	// После создания получаем экземпляр игры и сохраняем его в кэш
	newGame, err := g.gameUC.GetGameByID(ctx, gameKeySecret)
	if err != nil {
		g.log.Error("Не удалось получить игру после создания:", err)
	} else {
		activeGamesMu.Lock()
		activeGames[gameKeySecret] = &newGame
		activeGamesMu.Unlock()
	}

	resp := game.GameCreateResponse{
		UniqueKey: gameKeyPublic,
	}
	g.log.Info("New Game Created with keys: "+gameKeyPublic, gameKeySecret)
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, resp)
}

func (g *GameHandler) HandleJoinGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.log.Error("Only POST method is allowed")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		g.log.Error("Не нашли userID в куке")
		return
	}
	g.log.Infof("Join game request from id: %s", userID)

	var gameJoinRequest game.GameJoinRequest
	if err := utils.DecodeJSONRequest(r, &gameJoinRequest); err != nil {
		g.log.Error("JSON decode error:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	if gameJoinRequest.GameKeyPublic == "" || gameJoinRequest.Role == "" {
		g.log.Error("запрос на создание игры не содержит ключа игры или роли!")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Invalid JSON: ")
		return
	}

	ctx := r.Context()
	isAlreadyInGame, err := g.gameUC.HasUserActiveGamesByUserId(ctx, userID)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "ошибка при проверке на вхождение в уже существующую в игру: "+err.Error())
		return
	}
	if isAlreadyInGame {
		g.log.Error("пользователь уже состоит в игре!")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "ошибка при добавлении в игру: игрок уже состоит в игре")
		return
	}

	play, err := g.gameUC.GetGameByPublicKey(ctx, gameJoinRequest.GameKeyPublic)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "ошибка при получении игры: "+err.Error())
		return
	}

	if play.GameKeySecret == "" {
		g.log.Error("игра не найдена! Id: " + gameJoinRequest.GameKeyPublic)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "игра не найдена! Id: "+gameJoinRequest.GameKeyPublic)
		return
	}

	err = g.gameUC.JoinGame(ctx, gameJoinRequest, userID)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "ошибка при добавлении в игру: "+err.Error())
		return
	}

	// Обновляем кэш activeGames – если игра уже активна в памяти, обновляем информацию об игроках.
	activeGamesMu.Lock()
	if existingGame, ok := activeGames[gameJoinRequest.GameKeyPublic]; ok {
		if play.PlayerBlack == userID {
			existingGame.PlayerWhite = userID
		} else if play.PlayerWhite == userID {
			existingGame.PlayerBlack = userID
		}
	} else {
		// Если игры ещё нет в кэше, достаём из базы и добавляем
		retrievedGame, err := g.gameUC.GetGameByPublicKey(ctx, gameJoinRequest.GameKeyPublic)
		if err == nil {
			activeGames[gameJoinRequest.GameKeyPublic] = &retrievedGame
		}
	}
	activeGamesMu.Unlock()

	resp := JsonOKResponse{
		Text: "юзер успешно добавлен",
	}
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, resp)
}

func (g *GameHandler) HandleStartGame(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	gameID := r.URL.Query().Get("game_id")
	playerID := g.authHandler.GetUserID(w, r)
	if gameID == "" || playerID == "" {
		g.log.Error("отсутствуют поля gameID или playerID")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "отсутствуют поля gameID или playerID")
		return
	}
	g.log.Infof("Player id: %s", playerID)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade error:", err)
		return
	}

	// Сначала ищем игру в кэше, если её нет — достаём из базы и сохраняем в activeGames
	activeGamesMu.Lock()
	ag, ok := activeGames[gameID]
	if !ok {
		activeGamesMu.Unlock()
		retrievedGame, err := g.gameUC.GetGameByID(ctx, gameID)
		if err != nil {
			g.log.Error(err)
			httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
			return
		}
		activeGamesMu.Lock()
		activeGames[gameID] = &retrievedGame
		ag = &retrievedGame
	}
	activeGamesMu.Unlock()

	var playerWS **websocket.Conn
	var opponentWS **websocket.Conn
	switch playerID {
	case ag.PlayerBlack:
		playerWS, opponentWS = &ag.PlayerBlackWS, &ag.PlayerWhiteWS
	case ag.PlayerWhite:
		playerWS, opponentWS = &ag.PlayerWhiteWS, &ag.PlayerBlackWS
	default:
		g.log.Error("Unknown player id:", playerID)
		fmt.Println(ag)
		conn.Close()
		return
	}

	if *playerWS != nil {
		(*playerWS).WriteMessage(websocket.TextMessage, []byte("Вы были отключены, новое соединение создано."))
		(*playerWS).Close()
	}
	*playerWS = conn

	defer func() {
		conn.Close()
		activeGamesMu.Lock()
		if *playerWS == conn {
			*playerWS = nil
		}
		activeGamesMu.Unlock()
	}()

	for {
		var move game.Move
		if err = conn.ReadJSON(&move); err != nil {
			g.log.Error("read error:", err)
			return
		}
		g.log.Info("Получен ход: ", move)
		sgfString, err := g.gameUC.AddMoveToGameSgf(gameID, move)
		if err != nil {
			g.log.Error(err)
			conn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
			continue
		}
		resp := game.GameStateResponse{
			Move: move,
			SGF:  sgfString,
		}
		if *opponentWS != nil {
			if err := (*opponentWS).WriteJSON(resp); err != nil {
				g.log.Error("Write to opponent error:", err)
				(*opponentWS).Close()
				activeGamesMu.Lock()
				*opponentWS = nil
				activeGamesMu.Unlock()
			}
		} else {
			conn.WriteMessage(websocket.TextMessage, []byte("Оппонент не подключен"))
		}
	}
}

type JsonOKResponse struct {
	Text string `json:"text"`
}
