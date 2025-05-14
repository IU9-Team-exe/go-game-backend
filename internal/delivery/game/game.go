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
	Game       game.Game
	blackWS    *websocket.Conn
	whiteWS    *websocket.Conn
	spectators map[*websocket.Conn]struct{}
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

func (ag *activeGame) spectactorList() []*websocket.Conn {
	out := make([]*websocket.Conn, 0, len(ag.spectators))
	for ws := range ag.spectators {
		out = append(out, ws)
	}
	return out
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
// activeGame расширена полем spectators.
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

	activeGamesMu.Lock()
	ag, ok := activeGames[gameKey]
	if !ok {
		ag = &activeGame{
			Game:       *freshGame,
			spectators: make(map[*websocket.Conn]struct{}),
		}
		activeGames[gameKey] = ag
	} else {
		ag.Game = *freshGame
	}

	var role string              // "black" | "white" | "spectator"
	var slotPtr **websocket.Conn // ссылка на blackWS/whiteWS (если игрок)

	switch {
	case playerID == ag.Game.PlayerBlack:
		role, slotPtr = "black", &ag.blackWS
	case playerID == ag.Game.PlayerWhite:
		role, slotPtr = "white", &ag.whiteWS
	default:
		role = "spectator"
	}
	activeGamesMu.Unlock()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Errorf("WS upgrade error for %s in game %s: %v", playerID, gameKey, err)
		return
	}
	h.log.Infof("WS established for %s (%s) in game %s", playerID, role, gameKey)

	activeGamesMu.Lock()
	if role == "spectator" {
		ag.spectators[conn] = struct{}{}
	} else {
		if *slotPtr != nil {
			(*slotPtr).WriteMessage(websocket.TextMessage, []byte("reconnected"))
			(*slotPtr).Close()
		}
		*slotPtr = conn
	}
	activeGamesMu.Unlock()

	defer func() {
		activeGamesMu.Lock()
		if role == "spectator" {
			delete(ag.spectators, conn)
		} else {
			if role == "black" && ag.blackWS == conn {
				ag.blackWS = nil
			}
			if role == "white" && ag.whiteWS == conn {
				ag.whiteWS = nil
			}
		}
		activeGamesMu.Unlock()
		conn.Close()
	}()

	switch role {
	case "spectator":
		conn.WriteJSON(map[string]interface{}{
			"event":        "spectator_info",
			"player_black": ag.Game.PlayerBlack,
			"player_white": ag.Game.PlayerWhite,
		})
	default: // игрок
		var opponentID string
		activeGamesMu.Lock()
		if role == "black" {
			opponentID = ag.Game.PlayerWhite
		} else {
			opponentID = ag.Game.PlayerBlack
		}
		opponentWS := map[bool]*websocket.Conn{
			true:  ag.whiteWS,
			false: ag.blackWS,
		}[role == "black"]
		activeGamesMu.Unlock()

		if opponentID == "" || opponentWS == nil {
			conn.WriteJSON(map[string]string{
				"event": "waiting_for_opponent",
				"you":   playerID,
			})
		} else {
			if oppUser, err := h.authHandler.UsecaseHandler.GetUserByUserId(ctx, opponentID); err == nil {
				conn.WriteJSON(map[string]interface{}{
					"event": "opponent_info",
					"user":  oppUser,
				})
			}
			if me, err := h.authHandler.UsecaseHandler.GetUserByUserId(ctx, playerID); err == nil {
				opponentWS.WriteJSON(map[string]interface{}{
					"event": "opponent_joined",
					"user":  me,
				})
			}
		}
	}

	if role == "spectator" {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var move game.Move
		if err := json.Unmarshal(msg, &move); err != nil {
			continue
		}

		moveInfo, err := h.gameUC.AddMoveToGameSgf(ctx, ag.Game.GameKeySecret, move)
		if err != nil {
			conn.WriteMessage(websocket.TextMessage, []byte(err.Error()))
			continue
		}
		resp := game.GameStateResponse{Move: move, MoveInfo: *moveInfo}
		
		activeGamesMu.Lock()
		var oppWS *websocket.Conn
		if role == "black" {
			oppWS = ag.whiteWS
		} else {
			oppWS = ag.blackWS
		}
		spectators := make([]*websocket.Conn, 0, len(ag.spectators))
		for ws := range ag.spectators {
			spectators = append(spectators, ws)
		}
		activeGamesMu.Unlock()

		// отправляем
		if oppWS != nil {
			_ = oppWS.WriteJSON(resp)
		}
		for _, s := range spectators {
			_ = s.WriteJSON(resp)
		}

		// финал партии
		if resp.MoveInfo.IsGameFinished {
			finished := map[string]string{"event": "game_finished"}
			_ = conn.WriteJSON(finished)
			if oppWS != nil {
				_ = oppWS.WriteJSON(finished)
			}
			for _, s := range spectators {
				_ = s.WriteJSON(finished)
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
