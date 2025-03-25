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
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var activeGames = make(map[string]*game.Game)
var activeGamesMu sync.RWMutex

func NewGameHandler(cfg bootstrap.Config, log *zap.SugaredLogger, mongoAdapter *adapters.AdapterMongo, redisAdapter *adapters.AdapterRedis) *GameHandler {
	return &GameHandler{
		cfg:    cfg,
		log:    log,
		gameUC: gameuc.NewGameUseCase(repo.NewGameRepository(cfg, log, redisAdapter.GetClient(), mongoAdapter.Database)),
	}
}

func (g *GameHandler) HandleNewGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.log.Error("Only POST method is allowed")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

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
	}

	ctx := r.Context()
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

	if newGamerRequest.GameKey == "" || newGamerRequest.UserID == "" || newGamerRequest.Role == "" {
		g.log.Error("неверный json")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Invalid JSON: "+string(bodyBytes))
	}

	ctx := r.Context()

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
	defer conn.Close()

	ctx := r.Context()
	gameID := r.URL.Query().Get("game_id")
	playerID := r.URL.Query().Get("player_id")

	if gameID == "" || playerID == "" {
		log.Println("game_id and player_id required")
		return
	}

	activeGamesMu.Lock()
	ag, ok := activeGames[gameID]
	if !ok {
		foundGame, err := g.gameUC.GetGameByID(ctx, gameID)
		if err != nil {
			activeGamesMu.Unlock()
			g.log.Error(err)
			httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err)
			return
		}
		ag = &foundGame
		activeGames[gameID] = ag
	}
	activeGamesMu.Unlock()

	playerBID, playerWID := ag.PlayerBlack, ag.PlayerWhite

	if playerID == playerBID {
		if ag.PlayerBlackWS != nil {
			conn.WriteMessage(websocket.TextMessage, []byte("У вас уже есть активное подключение."))
			ag.PlayerBlackWS.Close()
			return
		}
		ag.PlayerBlackWS = conn
	} else if playerID == playerWID {
		if ag.PlayerWhiteWS != nil {
			conn.WriteMessage(websocket.TextMessage, []byte("У вас уже есть активное подключение."))
			ag.PlayerWhiteWS.Close()
			return
		}
		ag.PlayerWhiteWS = conn
	} else {
		g.log.Error("Unknown player id:", playerID)
		return
	}

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
			g.log.Info("отправили ход другому игроку")
			if err := opponentWS.WriteJSON(resp); err != nil {
				log.Println("Write to opponent error:", err)
			}
		}
		// Тут можно добавить сохранение хода в БД, redis
	}
}

type JsonOKResponse struct {
	Text string `json:"text"`
}
