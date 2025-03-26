package game

import (
	"bytes"
	"encoding/json"
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

	// проверка, что игрок свободен! TODO

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		g.log.Error("Failed to read body:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	g.log.Infof("Incoming JSON: %s", string(bodyBytes))

	var gameData game.Game
	decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
	decoder.DisallowUnknownFields()

	if err = decoder.Decode(&gameData); err != nil {
		g.log.Error("JSON decode error:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	if len(gameData.Users) != 1 {
		g.log.Error("неверный json")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Invalid JSON: "+string(bodyBytes))
		return
	}

	userID := g.authHandler.GetUserID(w, r)

	g.log.Infof("New game is from id: %s", userID)

	ctx := r.Context()

	alreadyIsInGame, err := g.gameUC.HasUserActiveGamesByUserId(ctx, userID)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "ошибка при добавлении в игру: "+err.Error())
		return
	}
	if alreadyIsInGame {
		g.log.Error("пользователь уже состоит в игре!") //TODO добавить отображение id игры
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "ошибка при добавлении в игру: уже состоит в игре")
		return
	}

	err, gameKey := g.gameUC.CreateGame(ctx, gameData)
	if err != nil {
		g.log.Error(err)
		return
	}

	resp := game.GameCreateResponse{
		UniqueKey: gameKey,
	}

	g.log.Info("New Game Created with key: " + gameKey)
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, resp)
}

func (g *GameHandler) HandleJoinGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.log.Error("Only POST method is allowed")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	userID := g.authHandler.GetUserID(w, r)

	g.log.Infof("New game is from id: %s", userID)

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		g.log.Error("Failed to read body:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	g.log.Infof("Incoming JSON: %s", string(bodyBytes))

	var newGamerRequest game.GameJoinRequest
	decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
	decoder.DisallowUnknownFields()

	if err = decoder.Decode(&newGamerRequest); err != nil {
		g.log.Error("JSON decode error:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Invalid JSON: "+err.Error())
		return
	}

	newGamerRequest.UserID = userID

	if newGamerRequest.GameKey == "" || newGamerRequest.UserID == "" || newGamerRequest.Role == "" {
		g.log.Error("неверный json")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Invalid JSON: "+string(bodyBytes))
		return
	}

	ctx := r.Context()

	alreadyIsInGame, err := g.gameUC.HasUserActiveGamesByUserId(ctx, newGamerRequest.UserID)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "ошибка при добавлении в игру: "+err.Error())
		return
	}
	if alreadyIsInGame {
		g.log.Error("пользователь уже состоит в игре!") //TODO добавить отображение id игры
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "ошибка при добавлении в игру: уже состоит в игре")
		return
	}

	err = g.gameUC.JoinGame(ctx, newGamerRequest)
	if err != nil {
		g.log.Error(err)
		return
	}

	resp := JsonOKResponse{
		Text: "юзер успешно добавлен",
	}

	g.log.Info(resp.Text)
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, resp)
}

func (g *GameHandler) HandleStartGame(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade error:", err)
		return
	}

	ctx := r.Context()
	gameID := r.URL.Query().Get("game_id")

	playerID := g.authHandler.GetUserID(w, r)

	if gameID == "" || playerID == "" {
		g.log.Error("отсутствуют поля gameID или playerID")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "отсутствуют поля gameID или playerID")
		return
	}

	if !g.gameUC.IsUserInGameByGameId(ctx, gameID, playerID) {
		g.log.Error("пользователь не в игре!")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "пользователь не в игре!")
		return
	}

	activeGamesMu.Lock()
	ag, ok := activeGames[gameID]
	if !ok {
		foundGame, err := g.gameUC.GetGameByID(ctx, gameID)
		if err != nil {
			activeGamesMu.Unlock()
			g.log.Error(err)
			httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
			return
		}
		ag = &foundGame
		activeGames[gameID] = ag
	}
	activeGamesMu.Unlock()

	playerBID, playerWID := ag.PlayerBlack, ag.PlayerWhite

	if playerID == playerBID {
		if ag.PlayerBlackWS != nil {
			_ = ag.PlayerBlackWS.WriteMessage(websocket.TextMessage, []byte("Вы были отключены, новое соединение создано."))
			_ = ag.PlayerBlackWS.Close()
		}
		ag.PlayerBlackWS = conn
	} else if playerID == playerWID {
		if ag.PlayerWhiteWS != nil {
			_ = ag.PlayerWhiteWS.WriteMessage(websocket.TextMessage, []byte("Вы были отключены, новое соединение создано."))
			_ = ag.PlayerWhiteWS.Close()
		}
		ag.PlayerWhiteWS = conn
	} else {
		g.log.Error("Unknown player id:", playerID)
		return
	}

	defer conn.Close()

	defer func() {
		activeGamesMu.Lock()
		defer activeGamesMu.Unlock()

		if ag.PlayerBlackWS == conn {
			ag.PlayerBlackWS = nil
		}
		if ag.PlayerWhiteWS == conn {
			ag.PlayerWhiteWS = nil
		}
	}()

	for {
		var move game.Move
		if err = conn.ReadJSON(&move); err != nil {
			g.log.Error("read error:", err)
			return
		}
		g.log.Info("Получен ход: ", move)

		var opponentWS *websocket.Conn
		if playerID == playerBID {
			opponentWS = ag.PlayerWhiteWS
		} else {
			opponentWS = ag.PlayerBlackWS
		}

		sgfString, err := g.gameUC.AddMoveToGameSgf(gameID, move)
		if err != nil {
			g.log.Error(err)
			conn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
		}

		resp := game.GameStateResponse{
			Move: move,
			SGF:  sgfString,
		}

		if opponentWS != nil {
			if err := opponentWS.WriteJSON(resp); err != nil {
				g.log.Error("Write to opponent error:", err)
				opponentWS.Close()

				activeGamesMu.Lock()
				if ag.PlayerBlackWS == opponentWS {
					ag.PlayerBlackWS = nil
				}
				if ag.PlayerWhiteWS == opponentWS {
					ag.PlayerWhiteWS = nil
				}
				activeGamesMu.Unlock()
			}
		}

	}
}

type JsonOKResponse struct {
	Text string `json:"text"`
}
