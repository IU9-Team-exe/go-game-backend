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
// @Summary Получить игру по публичному ключу
// @Description Возвращает подробную информацию об игре по публичному ключу, переданному в теле запроса.
// @Tags game
// @Accept json
// @Produce json
// @Param request body game.GetGameInfoRequest true "Запрос с публичным ключом игры"
// @Success 200 {object} game.GetGameInfoResponse "Успешное получение информации об игре"
// @Failure 400 {object} httpresponse.ErrorResponse "Неверный запрос или ошибка JSON"
// @Failure 500 {object} httpresponse.ErrorResponse "Внутренняя ошибка сервера"
// @Router /getGameByPublicKey [post]
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
		httpresponse.WriteResponseWithStatus(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, resp)
}

// HandleNewGame godoc
// @Summary Создать новую игру
// @Description Создает новую игру с указанными параметрами (размер доски, коми и роль). Требуется авторизация через cookie.
// @Tags game
// @Accept json
// @Produce json
// @Param request body game.CreateGameRequest true "Запрос на создание новой игры"
// @Success 200 {object} game.GameCreateResponse "Игра успешно создана"
// @Failure 400 {object} httpresponse.ErrorResponse "Неверный запрос"
// @Failure 405 {string} string "Разрешен только метод POST"
// @Router /NewGame [post]
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
// @Summary Покинуть игру
// @Description Позволяет пользователю покинуть игру, передав публичный ключ игры. Требуется авторизация через cookie.
// @Tags game
// @Accept json
// @Produce json
// @Param request body game.GameLeaveRequest true "Запрос на покидание игры"
// @Success 200 {string} string "Пользователь успешно покинул игру"
// @Failure 400 {object} httpresponse.ErrorResponse "Неверный запрос или ошибка JSON"
// @Failure 405 {string} string "Разрешен только метод POST"
// @Router /leave [post]
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

// HandleJoinGame godoc
// @Summary Присоединиться к игре
// @Description Позволяет пользователю присоединиться к игре, используя публичный ключ игры и роль. Требуется авторизация через cookie.
// @Tags game
// @Accept json
// @Produce json
// @Param request body game.GameJoinRequest true "Запрос на присоединение к игре"
// @Success 200 {object} JsonOKResponse "Пользователь успешно присоединился к игре"
// @Failure 400 {object} httpresponse.ErrorResponse "Неверный запрос или игра не найдена"
// @Failure 405 {string} string "Разрешен только метод POST"
// @Router /JoinGame [post]
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
// @Summary Запуск игры через WebSocket
// @Description Обновляет HTTP-соединение до WebSocket для обмена ходами в режиме реального времени.
// @Tags game
// @Accept json
// @Produce json
// @Param game_id query string true "Идентификатор игры"
// @Success 200 {object} game.GameStateResponse "Обновление состояния игры в реальном времени"
// @Failure 400 {object} httpresponse.ErrorResponse "Неверный запрос"
// @Router /startGame [get]
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
// @Summary Получить архив игр с пагинацией
// @Description Возвращает архив игр с постраничной разбивкой, с возможностью фильтрации по году или имени игрока. Обязательно необходимо указать хотя бы один из параметров: год (year) или имя (name).
// @Tags game
// @Accept json
// @Produce json
// @Param year query int false "Фильтр по году (обязателен, если не указан параметр name)"
// @Param name query string false "Фильтр по имени игрока (обязателен, если не указан параметр year)"
// @Param page query int false "Номер страницы для пагинации"
// @Success 200 {object} game.ArchiveResponse "Ответ с архивом игр с пагинацией"
// @Failure 400 {object} httpresponse.ErrorResponse "Неверный запрос или ошибка при получении архива"
// @Router /getArchive [get]
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
// @Summary Получить массив годов из архива
// @Description Возвращает отсортированный массив годов (int), доступных в архиве чужих партий.
// @Tags game
// @Accept json
// @Produce json
// @Success 200 {object} game.ArchiveYearsResponse "Ответ с массивом годов"
// @Failure 400 {object} httpresponse.ErrorResponse "Ошибка получения годов из архива"
// @Failure 405 {object} httpresponse.ErrorResponse "Метод не разрешен"
// @Router /getYearsInArchive [get]
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
// @Summary Получить массив годов из архива
// @Description Возвращает отсортированный массив годов (int), доступных в архиве чужих партий.
// @Tags game
// @Accept json
// @Produce json
// @Param page query int false "Номер страницы для пагинации"
// @Success 200 {object} game.ArchiveNamesResponse "Ответ с массивом годов"
// @Failure 400 {object} httpresponse.ErrorResponse "Ошибка получения годов из архива"
// @Failure 405 {object} httpresponse.ErrorResponse "Метод не разрешен"
// @Router /getNamesInArchive [get]
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
// @Summary Получить массив годов из архива
// @Description Возвращает отсортированный массив годов (int), доступных в архиве чужих партий.
// @Tags game
// @Accept json
// @Produce json
// @Param page query int false "Номер страницы для пагинации"
// @Success 200 {object} game.ArchiveNamesResponse "Ответ с массивом годов"
// @Failure 400 {object} httpresponse.ErrorResponse "Ошибка получения годов из архива"
// @Failure 405 {object} httpresponse.ErrorResponse "Метод не разрешен"
// @Router /getGameFromArchiveById [post]
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
