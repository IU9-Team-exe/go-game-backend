package game

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"team_exe/internal/adapters"
	"team_exe/internal/bootstrap"
	"team_exe/internal/delivery/auth"
	"team_exe/internal/domain/game"
	errs "team_exe/internal/errors"
	myErrors "team_exe/internal/errors"
	"team_exe/internal/httpresponse"
	repo "team_exe/internal/repository"
	gameuc "team_exe/internal/usecase/game"
	katagoUC "team_exe/internal/usecase/katago"
	"team_exe/internal/utils"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

type GameHandler struct {
	cfg         bootstrap.Config
	log         *zap.SugaredLogger
	gameUC      *gameuc.GameUseCase
	authHandler *auth.AuthHandler
}

type FindGameInArchive struct {
	GameId string `json:"game_id"`
}

type AlreadyInGameResponse struct {
	Error       string `json:"error"`
	CurrGameKey string `json:"currGameKey"`
}

type activeGame struct {
	Game    game.Game
	blackWS *websocket.Conn
	whiteWS *websocket.Conn
}

var (
	activeGames   = make(map[string]*activeGame)
	activeGamesMu sync.RWMutex
	upgrader      = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
)

type AnalyseGameRequest struct {
	GamePublicKey string `json:"game_public_key" bson:"game_public_key"`
}

type GenerateMoveReq struct {
	GameKeySecret string `json:"game_key_secret" bson:"game_key_secret"`
}

type JsonOKResponse struct {
	Text string `json:"text"`
}

// NewGameHandler создаёт новый обработчик игр.
func NewGameHandler(cfg bootstrap.Config, log *zap.SugaredLogger, mongoAdapter *adapters.AdapterMongo, redisAdapter *adapters.AdapterRedis, authHandler *auth.AuthHandler, katagoUC *katagoUC.KatagoUseCase) *GameHandler {
	return &GameHandler{
		cfg:         cfg,
		log:         log,
		gameUC:      gameuc.NewGameUseCase(repo.NewGameRepository(cfg, log, redisAdapter.GetClient(), mongoAdapter.Database), authHandler.UsecaseHandler, katagoUC),
		authHandler: authHandler,
	}
}

// HandleGetGameByPublicKey godoc
// @Summary      Retrieve game by public key
// @Description  Returns detailed information about a game given its public key.
// @Tags         game
// @Accept       json
// @Produce      json
// @Param        request  body      game.GetGameInfoRequest   true  "Game public key"
// @Success      200      {object}  game.GetGameInfoResponse  "Game information"
// @Failure      400      {object}  httpresponse.ErrorResponse "Bad request or invalid JSON"
// @Failure      500      {object}  httpresponse.ErrorResponse "Internal server error"
// @Router       /getGameByPublicKey [post]
func (h *GameHandler) HandleGetGameByPublicKey(w http.ResponseWriter, r *http.Request) {
	var req game.GetGameInfoRequest
	if err := utils.DecodeJSONRequest(r, &req); err != nil {
		h.log.Error("DecodeJSONRequest:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := h.gameUC.GetGameByPublicKey(r.Context(), req.GamePublicKey)
	if err != nil {
		h.log.Error("GetGameByPublicKey:", err)

		secResp, err := h.gameUC.GetGameBySecreteKey(r.Context(), req.GamePublicKey)
		if err != nil {
			httpresponse.WriteResponseWithStatus(w, http.StatusInternalServerError, err.Error())
			return
		}

		httpresponse.WriteResponseWithStatus(w, http.StatusOK, secResp)
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, resp)
}

// HandleNewGame godoc
// @Summary      Create a new game
// @Description  Creates a new Go game with the specified board size, komi and creator color. Requires authentication via cookie.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        request  body      game.CreateGameRequest    true  "New game parameters"
// @Success      200      {object}  game.GameCreateResponse  "Game successfully created"
// @Failure      400      {object}  httpresponse.ErrorResponse "Bad request (missing or invalid parameters)"
// @Failure      401      {string}  string                    "Unauthorized"
// @Failure      405      {string}  string                    "Method Not Allowed"
// @Router       /NewGame [post]
func (g *GameHandler) HandleNewGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.log.Error("Разрешен только метод POST")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Разрешен только метод POST")
		return
	}

	var newGameRequest game.CreateGameRequest
	if err := utils.DecodeJSONRequest(r, &newGameRequest); err != nil {
		g.log.Error("Ошибка декодирования JSON:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	if newGameRequest.BoardSize == 0 || newGameRequest.Komi == 0 { // TODO если по нулям, то выставляем дефолтные
		g.log.Error("Запрос на создание игры не содержит размер доски или коми")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Запрос не содержит размер доски или коми")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		g.log.Error("UserID не найден в cookie")
		httpresponse.WriteResponseWithStatus(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	//g.log.Infof("Новая игра от пользователя с id: %s", userID)

	ctx := r.Context()

	createdPlay, err := g.gameUC.CreateGame(ctx, newGameRequest, userID)
	if err != nil {
		if errors.Is(err, myErrors.ErrUserAlreadyInGame) {
			resp := AlreadyInGameResponse{
				Error:       "user already in game",
				CurrGameKey: createdPlay.GameKeyPublic,
			}
			httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, resp)
			return
		}
		httpresponse.WriteResponseWithStatus(w, http.StatusInternalServerError, err.Error())
		return
	}

	activeGamesMu.Lock()
	activeGames[createdPlay.GameKeySecret] = &activeGame{Game: *createdPlay}
	activeGamesMu.Unlock()

	resp := game.GameCreateResponse{
		PublicKey: createdPlay.GameKeyPublic,
	}
	g.log.Info("Новая игра создана с ключом: " + createdPlay.GameKeyPublic)
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, resp)
}

// LeaveGame godoc
// @Summary      Leave a game
// @Description  Allows a user to leave a game by its public key. Requires authentication via cookie.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        request  body      game.GameLeaveRequest     true  "Game public key"
// @Success      200      {string}  string                    "User successfully left the game"
// @Failure      400      {object}  httpresponse.ErrorResponse "Bad request or invalid JSON"
// @Failure      401      {string}  string                    "Unauthorized"
// @Failure      405      {string}  string                    "Method Not Allowed"
// @Router       /leaveGame [post]
func (g *GameHandler) LeaveGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.log.Error("Разрешен только метод POST")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Разрешен только метод POST")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		g.log.Error("UserID не найден в cookie")
		return
	}

	var gameLeaveRequest game.GameLeaveRequest
	if err := utils.DecodeJSONRequest(r, &gameLeaveRequest); err != nil {
		g.log.Error("Ошибка декодирования JSON:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	if gameLeaveRequest.GameKeyPublic == "" {
		g.log.Error("Запрос на покидание игры не содержит публичного ключа")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Запрос не содержит публичного ключа игры")
		return
	}

	ctx := r.Context()
	ok, err := g.gameUC.LeaveGame(ctx, gameLeaveRequest.GameKeyPublic, userID)
	if err != nil || !ok {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "Пользователь успешно покинул игру")
}

func (g *GameHandler) LeaveGameBot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.log.Error("Разрешен только метод POST")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Разрешен только метод POST")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		g.log.Error("UserID не найден в cookie")
		return
	}

	var gameLeaveRequest game.GameLeaveRequest
	if err := utils.DecodeJSONRequest(r, &gameLeaveRequest); err != nil {
		g.log.Error("Ошибка декодирования JSON:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	if gameLeaveRequest.GameKeyPublic == "" {
		g.log.Error("Запрос на покидание игры не содержит публичного ключа")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Запрос не содержит публичного ключа игры")
		return
	}

	ctx := r.Context()
	ok, err := g.gameUC.LeaveGame(ctx, gameLeaveRequest.GameKeyPublic, userID)
	if err != nil || !ok {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "Пользователь успешно покинул игру")
}

// HandleJoinGame godoc
// @Summary      Join a game
// @Description  Lets a user join an existing game by public key and role. Requires authentication via cookie.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        request  body      game.GameJoinRequest      true  "Join game parameters"
// @Success      200      {object}  JsonOKResponse            "User successfully joined"
// @Failure      400      {object}  httpresponse.ErrorResponse "Bad request or game not found"
// @Failure      401      {string}  string                    "Unauthorized"
// @Failure      405      {string}  string                    "Method Not Allowed"
// @Router       /JoinGame [post]
func (h *GameHandler) HandleJoinGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Only POST allowed")
		return
	}

	userID := h.authHandler.GetUserID(w, r)
	if userID == "" {
		httpresponse.WriteResponseWithStatus(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var req game.GameJoinRequest
	if err := utils.DecodeJSONRequest(r, &req); err != nil {
		h.log.Error("DecodeJSONRequest:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	joinedGame, err := h.gameUC.JoinGame(r.Context(), req.GameKeyPublic, req.Role, userID)
	if err != nil {
		if errors.Is(err, myErrors.ErrUserAlreadyInGame) {
			resp := AlreadyInGameResponse{
				Error:       "user already in game",
				CurrGameKey: joinedGame.GameKeyPublic,
			}
			httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, resp)
			return
		}
		httpresponse.WriteResponseWithStatus(w, http.StatusInternalServerError, err.Error())
		return
	}

	activeGamesMu.Lock()
	if ag, ok := activeGames[joinedGame.GameKeySecret]; ok {
		ag.Game = *joinedGame
	} else {
		activeGames[joinedGame.GameKeySecret] = &activeGame{Game: *joinedGame}
	}
	activeGamesMu.Unlock()

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, JsonOKResponse{Text: "Joined"})
}

// HandleStartGame godoc
// @Summary      Start real‑time game via WebSocket
// @Description  Upgrades the HTTP connection to WebSocket for live move exchange. Query param `game_id` required.
// @Tags         game
// @Security     ApiKeyAuth
// @Produce      json
// @Param        game_id  query     string                   true   "Public key of the game to join via WS"
// @Success      200      {object}  game.GameStateResponse  "Initial game state or move update"
// @Failure      400      {object}  httpresponse.ErrorResponse "Bad request (missing game_id)"
// @Failure      401      {string}  string                    "Unauthorized"
// @Failure      403      {string}  string                    "Forbidden (not a player)"
// @Failure      404      {string}  string                    "Not Found (game not found)"
// @Router       /startGame [get]
func (h *GameHandler) HandleStartGame(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1) Авторизация
	playerID := h.authHandler.GetUserID(w, r)
	if playerID == "" {
		h.log.Warn("HandleStartGame: unauthorized access")
		httpresponse.WriteResponseWithStatus(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// 2) Параметр game_id
	gameKey := r.URL.Query().Get("game_id")
	if gameKey == "" {
		h.log.Warnf("HandleStartGame: missing game_id (player %s)", playerID)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Missing game_id")
		return
	}

	// 3) Берём свежую игру и проверяем участника
	freshGame, err := h.gameUC.GetGameByPublicKey(ctx, gameKey)
	if err != nil {
		h.log.Errorf("HandleStartGame: GetGameByPublicKey(%s) failed: %v", gameKey, err)
		httpresponse.WriteResponseWithStatus(w, http.StatusNotFound, err.Error())
		return
	}
	if playerID != freshGame.PlayerBlack && playerID != freshGame.PlayerWhite {
		h.log.Warnf("HandleStartGame: player %s is not in game %s", playerID, gameKey)
		httpresponse.WriteResponseWithStatus(w, http.StatusForbidden, "Not a player")
		return
	}

	// 4) Обновляем activeGames
	h.log.Infof("HandleStartGame: preparing WS for player %s game %s", playerID, gameKey)
	activeGamesMu.Lock()
	ag, ok := activeGames[gameKey]
	if !ok {
		ag = &activeGame{Game: *freshGame}
		activeGames[gameKey] = ag
		h.log.Infof("HandleStartGame: created new activeGame entry for %s", gameKey)
	} else {
		ag.Game = *freshGame
		h.log.Infof("HandleStartGame: refreshed activeGame entry for %s", gameKey)
	}

	// 5) Определяем слот
	var slotPtr **websocket.Conn
	if playerID == ag.Game.PlayerBlack {
		slotPtr = &ag.blackWS
	} else {
		slotPtr = &ag.whiteWS
	}
	// Закрываем старое WS, если есть
	if old := *slotPtr; old != nil {
		h.log.Infof("HandleStartGame: closing old WS for player %s game %s", playerID, gameKey)
		old.WriteMessage(websocket.TextMessage, []byte("reconnected"))
		old.Close()
	}
	activeGamesMu.Unlock()

	// 6) Апгрейдим HTTP → WebSocket
	h.log.Infof("HandleStartGame: upgrading to WS for player %s game %s", playerID, gameKey)
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Errorf("HandleStartGame: WS upgrade error for player %s game %s: %v", playerID, gameKey, err)
		return
	}
	h.log.Infof("HandleStartGame: WS connection established for player %s game %s", playerID, gameKey)

	// 7) Дефер очистки
	defer func() {
		h.log.Infof("HandleStartGame: cleaning up WS for player %s game %s", playerID, gameKey)
		activeGamesMu.Lock()
		if ag2, ok2 := activeGames[gameKey]; ok2 {
			if ag2.blackWS == conn {
				ag2.blackWS = nil
			}
			if ag2.whiteWS == conn {
				ag2.whiteWS = nil
			}
		}
		activeGamesMu.Unlock()
		conn.Close()
	}()

	// 8) Сохраняем новый conn под защитой мьютекса
	activeGamesMu.Lock()
	*slotPtr = conn
	// Захватим свежего оппонента
	var opponentWS *websocket.Conn
	if playerID == ag.Game.PlayerBlack {
		opponentWS = ag.whiteWS
	} else {
		opponentWS = ag.blackWS
	}
	activeGamesMu.Unlock()
	h.log.Infof("HandleStartGame: assigned WS slot for player %s game %s (opponent connected: %v)", playerID, gameKey, opponentWS != nil)

	// 9) Цикл сообщений
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				h.log.Infof("HandleStartGame: client closed WS normally for player %s game %s: %v", playerID, gameKey, err)
			} else {
				h.log.Errorf("HandleStartGame: WS read error for player %s game %s: %v", playerID, gameKey, err)
			}
			return
		}

		var move game.Move
		if err := json.Unmarshal(msg, &move); err != nil {
			h.log.Warnf("HandleStartGame: invalid JSON from player %s game %s: %v", playerID, gameKey, err)
			continue
		}
		h.log.Infof("HandleStartGame: received move from %s in game %s: %+v", playerID, gameKey, move)

		moveInfo, err := h.gameUC.AddMoveToGameSgf(ctx, ag.Game.GameKeySecret, move)
		if err != nil {
			h.log.Errorf("HandleStartGame: AddMoveToGameSgf error for player %s game %s: %v", playerID, gameKey, err)
			conn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
			continue
		}

		resp := game.GameStateResponse{Move: move, MoveInfo: *moveInfo}
		h.log.Debugf("HandleStartGame: sending response %+v to opponent in game %s", resp, gameKey)

		activeGamesMu.Lock()
		var opponentWS *websocket.Conn
		if playerID == ag.Game.PlayerBlack {
			opponentWS = ag.whiteWS
		} else {
			opponentWS = ag.blackWS
		}
		activeGamesMu.Unlock()

		if opponentWS != nil {
			if err := opponentWS.WriteJSON(resp); err != nil {
				h.log.Errorf("HandleStartGame: failed to send to opponent in game %s: %v", gameKey, err)
				opponentWS.Close()
				activeGamesMu.Lock()
				if playerID == ag.Game.PlayerBlack {
					ag.whiteWS = nil
				} else {
					ag.blackWS = nil
				}
				activeGamesMu.Unlock()
			}
		} else {
			conn.WriteMessage(websocket.TextMessage, []byte("Opponent not connected"))
		}

		if resp.MoveInfo.IsGameFinished {
			h.log.Infof("HandleStartGame: game finished for game %s", gameKey)
			finished := map[string]string{"event": "game_finished"}
			conn.WriteJSON(finished)
			if opponentWS != nil {
				opponentWS.WriteJSON(finished)
			}
			return
		}
	}
}

// HandleGetArchivePaginator godoc
// @Summary      List archived games with pagination
// @Description  Returns a page of archived games, filterable by year or player name (at least one required).
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        year     query     int     false  "Filter by year (required if name not set)"
// @Param        name     query     string  false  "Filter by player name (required if year not set)"
// @Param        page     query     int     false  "Page number (default 0)"
// @Success      200      {object}  game.ArchiveResponse      "Page of archived games"
// @Failure      400      {object}  httpresponse.ErrorResponse "Bad request or archive retrieval error"
// @Failure      405      {string}  string                    "Method Not Allowed"
// @Router       /getArchive [get]
func (g *GameHandler) HandleGetArchivePaginator(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.log.Error("Разрешен только метод GET")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Разрешен только метод GET")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		g.log.Error("UserID не найден в cookie")
		return
	}

	year := r.URL.Query().Get("year")
	name := r.URL.Query().Get("name")
	page := r.URL.Query().Get("page")

	yearNum := 0
	var err error
	if year != "" {
		yearNum, err = strconv.Atoi(year)
		if err != nil {
			g.log.Error(err)
			httpresponse.WriteResponseWithStatus(w, 400, fmt.Errorf("ошибка преобразования года: "+err.Error()))
			return
		}
	}

	pageNum := 0
	if page != "" {
		pageNum, err = strconv.Atoi(page)
		if err != nil {
			g.log.Error(err)
			httpresponse.WriteResponseWithStatus(w, 400, fmt.Errorf("ошибка преобразования номера страницы: "+err.Error()))
			return
		}
	}

	ctx := r.Context()
	resp, err := g.gameUC.GetArchiveOfGames(ctx, pageNum, yearNum, name)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, 400, fmt.Errorf("ошибка получения архива: "+err.Error()))
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, resp)
}

// HandleGetYearsInArchive godoc
// @Summary      List available archive years
// @Description  Returns a sorted list of years for which archived games exist.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Success      200      {object}  game.ArchiveYearsResponse  "Array of years"
// @Failure      400      {object}  httpresponse.ErrorResponse "Error retrieving years"
// @Failure      405      {object}  httpresponse.ErrorResponse "Method Not Allowed"
// @Router       /getYearsInArchive [get]
func (g *GameHandler) HandleGetYearsInArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.log.Error("Разрешен только метод GET")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Разрешен только метод GET")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		g.log.Error("UserID не найден в cookie")
		httpresponse.WriteResponseWithStatus(w, http.StatusUnauthorized, "UserID не найден в cookie")
		return
	}

	ctx := r.Context()
	resp, err := g.gameUC.GetListOfArchiveYears(ctx)
	if err != nil {
		g.log.Error("Ошибка получения годов из архива: ", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, fmt.Sprintf("ошибка получения годов из архива: %v", err))
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, resp)
}

// HandleGetNamesInArchive godoc
// @Summary      List most frequent players in archive
// @Description  Returns a paginated list of player names sorted by game count.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        page     query     int     false  "Page number (default 1)"
// @Success      200      {object}  game.ArchiveNamesResponse  "Page of player names"
// @Failure      400      {object}  httpresponse.ErrorResponse "Error retrieving names"
// @Failure      405      {object}  httpresponse.ErrorResponse "Method Not Allowed"
// @Router       /getNamesInArchive [get]
func (g *GameHandler) HandleGetNamesInArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.log.Error("Разрешен только метод GET")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Разрешен только метод GET")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		g.log.Error("UserID не найден в cookie")
		httpresponse.WriteResponseWithStatus(w, http.StatusUnauthorized, "UserID не найден в cookie")
		return
	}

	pageNum := r.URL.Query().Get("page")
	pageNumInt, err := strconv.Atoi(pageNum)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, 400, fmt.Errorf("ошибка преобразования года: "+err.Error()))
		return
	}

	ctx := r.Context()
	resp, err := g.gameUC.GetListOfArchiveNames(ctx, pageNumInt)
	if err != nil {
		g.log.Error("Ошибка получения игроков из архива: ", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, fmt.Sprintf("ошибка получения игроков из архива: %v", err))
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, resp)
}

// HandleGetGameFromArchiveById godoc
// @Summary      Retrieve a single archived game by ID
// @Description  Returns the archived game record for the given archive document ID.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        request  body      FindGameInArchive          true  "Archive document ID"
// @Success      200      {object}  game.GameFromArchive      "Archived game details"
// @Failure      400      {object}  httpresponse.ErrorResponse "Bad request or invalid JSON"
// @Failure      401      {string}  string                    "Unauthorized"
// @Failure      405      {string}  string                    "Method Not Allowed"
// @Router       /getGameFromArchiveById [post]
func (g *GameHandler) HandleGetGameFromArchiveById(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.log.Error("Разрешен только метод POST")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Разрешен только метод POST")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		g.log.Error("UserID не найден в cookie")
		httpresponse.WriteResponseWithStatus(w, http.StatusUnauthorized, "UserID не найден в cookie")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		g.log.Error("Ошибка чтения тела запроса:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Ошибка чтения тела запроса")
		return
	}
	defer r.Body.Close()

	decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
	decoder.DisallowUnknownFields()

	var findGameReq FindGameInArchive
	if err = decoder.Decode(&findGameReq); err != nil {
		g.log.Error("Ошибка декодирования JSON:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Неверный JSON: "+err.Error())
		return
	}

	ctx := r.Context()
	foundGame, err := g.gameUC.GetGameFromArchiveById(ctx, findGameReq.GameId)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Ошибка при получении игры: "+err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, foundGame)
}

// HandleAnalyseOfCurrentGame godoc
// @Summary      Analyse current game state
// @Description  Sends current game SGF to KataGo for analysis. Requires authentication.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        request  body      AnalyseGameRequest         true  "Public key of current game"
// @Success      200      {object}  game.KataGoResponse        "KataGo analysis results"
// @Failure      400      {object}  httpresponse.ErrorResponse "Bad request or analysis error"
// @Failure      401      {string}  string                    "Unauthorized"
// @Failure      405      {string}  string                    "Method Not Allowed"
// @Router       /analyseCurrent [post]
func (g *GameHandler) HandleAnalyseOfCurrentGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.log.Error("Разрешен только метод POST")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Разрешен только метод POST")
		return
	}

	var analyseReq AnalyseGameRequest
	if err := utils.DecodeJSONRequest(r, &analyseReq); err != nil {
		g.log.Error("Ошибка декодирования JSON:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	if analyseReq.GamePublicKey == "" { // TODO если по нулям, то выставляем дефолтные
		g.log.Error("Запрос на анализ игры содержит некорректный публичный ключ игры")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Запрос на анализ игры содержит некорректный публичный ключ игры")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		g.log.Error("UserID не найден в cookie")
		httpresponse.WriteResponseWithStatus(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	ctx := r.Context()

	analyseResp, err := g.gameUC.AnalyseCurrentGame(ctx, userID, analyseReq.GamePublicKey)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err)
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, analyseResp)
}

type GenerateMoveRequest struct {
	Move game.Move `json:"move"`
}

type CreateBotGameRequest struct {
	BoardSize      int     `json:"board_size"`
	Komi           float64 `json:"komi"`
	Rules          string  `json:"rules"`
	IsCreatorBlack bool    `json:"is_creator_black"`
}

func (h *GameHandler) HandleGenerateMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Only POST allowed")
		return
	}

	var req GenerateMoveRequest
	if err := utils.DecodeJSONRequest(r, &req); err != nil {
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	userID := h.authHandler.GetUserID(w, r)
	if userID == "" {
		h.log.Error("UserID не найден в cookie")
		httpresponse.WriteResponseWithStatus(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	ctx := r.Context()

	gameId, err := h.gameUC.GetActiveGameSecretKey(ctx, userID)
	if err != nil {
		h.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err)
		return
	}

	allMoves, newSgf, err := h.gameUC.GenerateMoveAgainstBot(r.Context(), gameId, req.Move)
	if err != nil {
		h.log.Error("GenerateMoveAgainstBot:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, map[string]interface{}{
		"moves": allMoves,
		"sgf":   newSgf,
	})
}

// POST /newBotGame
func (h *GameHandler) HandleNewBotGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Only POST allowed")
		return
	}
	var req CreateBotGameRequest
	if err := utils.DecodeJSONRequest(r, &req); err != nil {
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}
	userID := h.authHandler.GetUserID(w, r)
	if userID == "" {
		httpresponse.WriteResponseWithStatus(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	gameObj, err := h.gameUC.CreateBotGame(r.Context(), game.CreateGameRequest{
		BoardSize:      req.BoardSize,
		Komi:           req.Komi,
		Rules:          req.Rules,
		IsCreatorBlack: req.IsCreatorBlack,
	}, userID)
	if err != nil {
		if errors.Is(err, errs.ErrUserAlreadyInGame) {
			httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, map[string]string{
				"error":      "bot game already exists",
				"secret_key": gameObj.GameKeySecret,
			})
			return
		}
		h.log.Error("CreateBotGame:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, map[string]string{
		"secret_key": gameObj.GameKeySecret,
	})
}
