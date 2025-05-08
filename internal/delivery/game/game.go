package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"team_exe/internal/adapters"
	"team_exe/internal/bootstrap"
	"team_exe/internal/delivery/auth"
	"team_exe/internal/domain/game"
	errs "team_exe/internal/errors"
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

type BotGenerateMoveResponse struct {
	Moves []game.Move `json:"moves"`
	Sgf   string      `json:"sgf"`
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
	GameSecretKey string `json:"game_secret_key" bson:"game_secret_key"`
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
// @Summary      Получить игру по публичному ключу
// @Description  Возвращает подробную информацию об игре по её публичному ключу.
// @Tags         game
// @Accept       json
// @Produce      json
// @Param        request  body      game.GetGameInfoRequest               true  "Публичный ключ игры"
// @Success      200      {object}  httpresponse.Response{Body=game.GetGameInfoResponse}  "Информация об игре"
// @Failure      400      {object}  httpresponse.Response                "Некорректный запрос или ошибка JSON"
// @Failure      404      {object}  httpresponse.Response                "Игра не найдена"
// @Failure      500      {object}  httpresponse.Response                "Внутренняя ошибка сервера"
// @Router       /getGameByPublicKey [post]
func (h *GameHandler) HandleGetGameByPublicKey(w http.ResponseWriter, r *http.Request) {
	var req game.GetGameInfoRequest
	if err := utils.DecodeJSONRequest(r, &req); err != nil {
		h.log.Error("DecodeJSONRequest:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}

	ctx := r.Context()

	play, err := h.gameUC.GetGameByPublicKey(ctx, req.GamePublicKey)
	if err != nil {
		// вдруг клиент прислал secret key
		if alt, altErr := h.gameUC.GetGameBySecreteKey(ctx, req.GamePublicKey); altErr == nil {
			httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", alt)
			return
		}

		h.log.Error("GetGameByPublicKey:", err)
		status, code := errs.TranslateErr(err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", play)
}

// HandleNewGame godoc
// @Summary      Создать новую игру человек vs человек
// @Description  Регистрирует новую игру и возвращает её публичный ключ.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        payload  body      game.CreateGameRequest               true  "Параметры новой игры"
// @Success      200      {object}  httpresponse.Response{Body=game.GameCreateResponse}   "Публичный ключ игры"
// @Failure      400      {object}  httpresponse.Response                "Ошибочные параметры"
// @Failure      401      {object}  httpresponse.Response                "Неавторизован"
// @Failure      409      {object}  httpresponse.Response{Body=game.AlreadyInGameResponse}  "Пользователь уже в игре"
// @Failure      405      {object}  httpresponse.Response                "Метод не разрешён"
// @Router       /NewGame [post]
func (g *GameHandler) HandleNewGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		g.log.Error("Разрешен только метод POST")
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed")
		return
	}

	var newGameRequest game.CreateGameRequest
	if err := utils.DecodeJSONRequest(r, &newGameRequest); err != nil {
		g.log.Error("Ошибка декодирования JSON:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}

	if newGameRequest.BoardSize == 0 || newGameRequest.Komi == 0 {
		g.log.Error("Запрос не содержит размер доски или коми")
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "invalid_parameters", "BoardSize and Komi are required")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		g.log.Error("UserID не найден в cookie")
		httpresponse.WriteAPIError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	createdPlay, err := g.gameUC.CreateGame(r.Context(), newGameRequest, userID)
	if err != nil {
		status, code := errs.TranslateErr(err)
		// если это ErrUserAlreadyInGame, дополняем тело данными
		if errors.Is(err, errs.ErrUserAlreadyInGame) && createdPlay != nil {
			resp := AlreadyInGameResponse{
				Error:       code,
				CurrGameKey: createdPlay.GameKeyPublic,
			}
			httpresponse.WriteResponseWithStatus(w, status, code, resp)
			return
		}
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	activeGamesMu.Lock()
	activeGames[createdPlay.GameKeySecret] = &activeGame{Game: *createdPlay}
	activeGamesMu.Unlock()

	resp := game.GameCreateResponse{PublicKey: createdPlay.GameKeyPublic}
	g.log.Infof("Новая игра создана с ключом: %s", createdPlay.GameKeyPublic)
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", resp)
}

// HandleLeaveGame godoc
// @Summary      Покинуть игру
// @Description  Покидает текущую игру по публичному ключу.
// @Tags         game
// @Security     ApiKeyAuth
// @Produce      json
// @Param        gameKey  query     string                             true  "Публичный ключ игры"
// @Success      200      {object}  httpresponse.Response{Body=game.JsonOKResponse}      "Успешно вышел из игры"
// @Failure      400      {object}  httpresponse.Response                "Ошибочный запрос"
// @Failure      401      {object}  httpresponse.Response                "Неавторизован"
// @Failure      405      {object}  httpresponse.Response                "Метод не разрешён"
// @Router       /leaveGame [get]
func (g *GameHandler) HandleLeaveGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.log.Error("Разрешен только метод GET")
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET allowed")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		g.log.Error("UserID не найден в cookie")
		httpresponse.WriteAPIError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	gameKey := r.URL.Query().Get("gameKey")
	ok, err := g.gameUC.LeaveGame(r.Context(), userID, gameKey)
	if err != nil || !ok {
		status, code := errs.TranslateErr(err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", JsonOKResponse{Text: "Left game"})
}

// HandleJoinGame godoc
// @Summary      Присоединиться к игре
// @Description  Добавляет пользователя в существующую игру.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        payload  body      game.GameJoinRequest                 true  "Публичный ключ и роль"
// @Success      200      {object}  httpresponse.Response{Body=game.JsonOKResponse}      "Успешно присоединился"
// @Failure      400      {object}  httpresponse.Response                "Игра не найдена или неверные параметры"
// @Failure      401      {object}  httpresponse.Response                "Неавторизован"
// @Failure      409      {object}  httpresponse.Response{Body=game.AlreadyInGameResponse}  "Пользователь уже в игре"
// @Failure      405      {object}  httpresponse.Response                "Метод не разрешён"
// @Router       /JoinGame [post]
func (h *GameHandler) HandleJoinGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed")
		return
	}

	userID := h.authHandler.GetUserID(w, r)
	if userID == "" {
		httpresponse.WriteAPIError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	var req game.GameJoinRequest
	if err := utils.DecodeJSONRequest(r, &req); err != nil {
		h.log.Error("DecodeJSONRequest:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}

	joinedGame, err := h.gameUC.JoinGame(r.Context(), req.GameKeyPublic, req.Role, userID)
	if err != nil {
		status, code := errs.TranslateErr(err)
		if errors.Is(err, errs.ErrUserAlreadyInGame) && joinedGame != nil {
			resp := AlreadyInGameResponse{
				Error:       code,
				CurrGameKey: joinedGame.GameKeyPublic,
			}
			httpresponse.WriteResponseWithStatus(w, status, code, resp)
			return
		}
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	activeGamesMu.Lock()
	if ag, ok := activeGames[joinedGame.GameKeySecret]; ok {
		ag.Game = *joinedGame
	} else {
		activeGames[joinedGame.GameKeySecret] = &activeGame{Game: *joinedGame}
	}
	activeGamesMu.Unlock()

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", JsonOKResponse{Text: "Joined"})
}

// HandleStartGame godoc
// @Summary      WebSocket-сессия игры
// @Description  Апгрейд HTTP→WS для обмена ходами в реальном времени.
// @Tags         game
// @Security     ApiKeyAuth
// @Produce      plain
// @Param        game_id  query     string                             true  "Публичный ключ игры"
// @Success      101      {string}  string                              "Переключение протокола"
// @Failure      400      {object}  httpresponse.Response                "Ошибка запроса"
// @Failure      401      {object}  httpresponse.Response                "Неавторизован"
// @Failure      403      {object}  httpresponse.Response                "Пользователь не в этой игре"
// @Router       /startGame [get]
func (h *GameHandler) HandleStartGame(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	playerID := h.authHandler.GetUserID(w, r)
	if playerID == "" {
		httpresponse.WriteAPIError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	gameKey := r.URL.Query().Get("game_id")
	if gameKey == "" {
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "missing_game_id", "game_id is required")
		return
	}

	freshGame, err := h.gameUC.GetGameByPublicKey(ctx, gameKey)
	if err != nil {
		status, code := errs.TranslateErr(err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	if playerID != freshGame.PlayerBlack && playerID != freshGame.PlayerWhite {
		h.log.Warnf("HandleStartGame: player %s is not in game %s", playerID, gameKey)
		httpresponse.WriteAPIError(w, http.StatusForbidden, "user_not_in_this_game", "You are not a participant of this game")
		return
	}

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
	var opponentID string
	var opponentWS *websocket.Conn

	if playerID == ag.Game.PlayerBlack {
		opponentWS = ag.whiteWS
		opponentID = ag.Game.PlayerWhite
	} else {
		opponentWS = ag.blackWS
		opponentID = ag.Game.PlayerBlack
	}
	activeGamesMu.Unlock()

	if opponentWS == nil || opponentID == "" {
		// никто ещё не присоединился
		conn.WriteJSON(map[string]string{
			"event": "waiting_for_opponent",
			"you":   playerID,
		})
	} else {
		oppUser, err := h.authHandler.UsecaseHandler.GetUserByUserId(ctx, opponentID)
		if err != nil {
			conn.WriteJSON(map[string]string{"event": "error", "msg": "cannot fetch opponent"})
		} else {
			conn.WriteJSON(map[string]interface{}{
				"event": "opponent_info",
				"user":  oppUser,
			})
		}
	}

	if opponentWS != nil {
		me, err := h.authHandler.UsecaseHandler.GetUserByUserId(ctx, playerID)
		if err == nil {
			opponentWS.WriteJSON(map[string]interface{}{
				"event": "opponent_joined",
				"user":  me,
			})
		}
	}

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
// @Summary      Список игр из архива
// @Description  Возвращает архивные игры с фильтрацией и пагинацией.
// @Tags         game
// @Security     ApiKeyAuth
// @Produce      json
// @Param        year     query     int                                false "Фильтр по году"
// @Param        name     query     string                             false "Фильтр по имени игрока"
// @Param        page     query     int                                false "Номер страницы"
// @Success      200      {object}  httpresponse.Response{Body=game.ArchiveResponse} "Страница архивных игр"
// @Failure      400      {object}  httpresponse.Response                "Некорректный запрос"
// @Failure      401      {object}  httpresponse.Response                "Неавторизован"
// @Failure      405      {object}  httpresponse.Response                "Метод не разрешён"
// @Router       /getArchive [get]
func (g *GameHandler) HandleGetArchivePaginator(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET allowed")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		httpresponse.WriteAPIError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	yearParam := r.URL.Query().Get("year")
	name := r.URL.Query().Get("name")
	pageParam := r.URL.Query().Get("page")

	year, err := strconv.Atoi(yearParam)
	if yearParam != "" && err != nil {
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "invalid_year", err.Error())
		return
	}

	page, err := strconv.Atoi(pageParam)
	if pageParam != "" && err != nil {
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "invalid_page", err.Error())
		return
	}

	resp, err := g.gameUC.GetArchiveOfGames(r.Context(), page, year, name)
	if err != nil {
		status, code := errs.TranslateErr(err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", resp)
}

// HandleGetYearsInArchive godoc
// @Summary      Список годов в архиве
// @Description  Возвращает все года, за которые есть игры в архиве.
// @Tags         game
// @Security     ApiKeyAuth
// @Produce      json
// @Success      200      {object}  httpresponse.Response{Body=game.ArchiveYearsResponse}  "Список годов"
// @Failure      401      {object}  httpresponse.Response                "Неавторизован"
// @Failure      405      {object}  httpresponse.Response                "Метод не разрешён"
// @Router       /getYearsInArchive [get]
func (g *GameHandler) HandleGetYearsInArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET allowed")
		return
	}
	if g.authHandler.GetUserID(w, r) == "" {
		httpresponse.WriteAPIError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	resp, err := g.gameUC.GetListOfArchiveYears(r.Context())
	if err != nil {
		status, code := errs.TranslateErr(err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", resp)
}

// HandleGetNamesInArchive godoc
// @Summary      Список игроков в архиве
// @Description  Возвращает имена игроков, отсортированные по количеству игр.
// @Tags         game
// @Security     ApiKeyAuth
// @Produce      json
// @Param        page     query     int                                false "Номер страницы"
// @Success      200      {object}  httpresponse.Response{Body=game.ArchiveNamesResponse}  "Страница имён игроков"
// @Failure      401      {object}  httpresponse.Response                "Неавторизован"
// @Failure      405      {object}  httpresponse.Response                "Метод не разрешён"
// @Router       /getNamesInArchive [get]
func (g *GameHandler) HandleGetNamesInArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET allowed")
		return
	}
	if g.authHandler.GetUserID(w, r) == "" {
		httpresponse.WriteAPIError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	pageParam := r.URL.Query().Get("page")
	page, err := strconv.Atoi(pageParam)
	if pageParam != "" && err != nil {
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "invalid_page", err.Error())
		return
	}

	resp, err := g.gameUC.GetListOfArchiveNames(r.Context(), page)
	if err != nil {
		status, code := errs.TranslateErr(err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", resp)
}

// HandleGetGameFromArchiveById godoc
// @Summary      Получить игру из архива по ID
// @Description  Возвращает запись об архивной игре по её идентификатору.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        payload  body      FindGameInArchive                    true  "Идентификатор архива"
// @Success      200      {object}  httpresponse.Response{Body=game.GameFromArchive}  "Данные игры из архива"
// @Failure      400      {object}  httpresponse.Response                "Некорректный запрос"
// @Failure      401      {object}  httpresponse.Response                "Неавторизован"
// @Failure      405      {object}  httpresponse.Response                "Метод не разрешён"
// @Router       /getGameFromArchiveById [post]
func (g *GameHandler) HandleGetGameFromArchiveById(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed")
		return
	}
	if g.authHandler.GetUserID(w, r) == "" {
		httpresponse.WriteAPIError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	var req FindGameInArchive
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}

	resp, err := g.gameUC.GetGameFromArchiveById(r.Context(), req.GameId)
	if err != nil {
		status, code := errs.TranslateErr(err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", resp)
}

// HandleAnalyseGame godoc
// @Summary      Анализ текущей игры
// @Description  Отправляет SGF в KataGo для анализа (по умолчанию — активная игра).
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        secret_key  query     string                        false "Секретный ключ игры"
// @Success      200         {object}  httpresponse.Response{Body=game.KataGoResponse}  "Результаты анализа"
// @Failure      400         {object}  httpresponse.Response                "Некорректный запрос"
// @Failure      401         {object}  httpresponse.Response                "Неавторизован"
// @Failure      405         {object}  httpresponse.Response                "Метод не разрешён"
// @Router       /analyseCurrent [get]
func (g *GameHandler) HandleAnalyseGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET allowed")
		return
	}
	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		httpresponse.WriteAPIError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	secret := r.URL.Query().Get("game_key")
	if secret == "" {
		var err error
		secret, err = g.gameUC.GetActiveGameSecretKey(r.Context(), userID, false)
		if err != nil {
			status, code := errs.TranslateErr(err)
			httpresponse.WriteAPIError(w, status, code, err.Error())
			return
		}
	}

	resp, err := g.gameUC.AnalyseCurrentGame(r.Context(), secret)
	if err != nil {
		status, code := errs.TranslateErr(err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", resp)
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

// HandleGenerateMove godoc
// @Summary      Ход против бота
// @Description  Принимает ход пользователя, генерирует ход бота, возвращает все ходы и SGF.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        payload  body      GenerateMoveRequest                 true  "Ход пользователя"
// @Success      200      {object}  httpresponse.Response{Body=BotGenerateMoveResponse}  "Список ходов и SGF"
// @Failure      400      {object}  httpresponse.Response                "Неверный JSON или логика игры"
// @Failure      401      {object}  httpresponse.Response                "Неавторизован"
// @Failure      405      {object}  httpresponse.Response                "Метод не разрешён"
// @Failure      500      {object}  httpresponse.Response                "Внутренняя ошибка сервера"
// @Router       /generateMove [post]
func (h *GameHandler) HandleGenerateMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed")
		return
	}
	userID := h.authHandler.GetUserID(w, r)
	if userID == "" {
		httpresponse.WriteAPIError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	var req GenerateMoveRequest
	if err := utils.DecodeJSONRequest(r, &req); err != nil {
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}

	secret, err := h.gameUC.GetActiveGameSecretKey(r.Context(), userID, true)
	if err != nil {
		status, code := errs.TranslateErr(err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	moves, sgf, err := h.gameUC.GenerateMoveAgainstBot(r.Context(), secret, req.Move)
	if err != nil {
		status, code := errs.TranslateErr(err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", BotGenerateMoveResponse{Moves: moves, Sgf: sgf})
}

// HandleNewBotGame godoc
// @Summary      Создать игру с ботом
// @Description  Создаёт игру против KataGo и возвращает секретный ключ.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        payload  body      CreateBotGameRequest                true  "Параметры новой игры с ботом"
// @Success      200      {object}  httpresponse.Response{Body=map[string]string}     "Секретный ключ игры"
// @Failure      400      {object}  httpresponse.Response                "Неверный запрос"
// @Failure      401      {object}  httpresponse.Response                "Неавторизован"
// @Failure      405      {object}  httpresponse.Response                "Метод не разрешён"
// @Failure      409      {object}  httpresponse.Response                "Бот-игра уже существует"
// @Router       /newBotGame [post]
func (h *GameHandler) HandleNewBotGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed")
		return
	}
	userID := h.authHandler.GetUserID(w, r)
	if userID == "" {
		httpresponse.WriteAPIError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	var req CreateBotGameRequest
	if err := utils.DecodeJSONRequest(r, &req); err != nil {
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}

	gameObj, err := h.gameUC.CreateBotGame(r.Context(), game.CreateGameRequest{
		BoardSize:      req.BoardSize,
		Komi:           req.Komi,
		Rules:          req.Rules,
		IsCreatorBlack: req.IsCreatorBlack,
	}, userID)
	if err != nil {
		status, code := errs.TranslateErr(err)
		if errors.Is(err, errs.ErrUserAlreadyInGame) {
			httpresponse.WriteAPIError(w, status, code, fmt.Sprintf("bot game already exists: %s", gameObj.GameKeySecret))
			return
		}
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", map[string]string{"secret_key": gameObj.GameKeySecret})
}
