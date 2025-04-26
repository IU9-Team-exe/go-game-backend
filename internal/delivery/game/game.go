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
// @Param        request  body      game.GetGameInfoRequest   true  "Публичный ключ игры"
// @Success      200      {object}  game.GetGameInfoResponse  "Информация об игре"
// @Failure      400      {object}  httpresponse.ErrorResponse "Некорректный запрос или ошибка в формате JSON"
// @Failure      500      {object}  httpresponse.ErrorResponse "Внутренняя ошибка сервера"
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
// @Summary      Создать новую игру человека против человека
// @Description  Регистрирует новую запись в базе и отдаёт публичный ключ партии. Нужна авторизация.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        payload  body      game.CreateGameRequest     true  "Параметры игры"
// @Success      200      {object}  game.GameCreateResponse
// @Failure      400      {object}  httpresponse.Response      "Ошибочные параметры или пользователь уже в игре"
// @Failure      401      {object}  httpresponse.Response      "Нет cookie sessionID"
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

// HandleLeaveGame godoc
// @Summary      Покинуть игру
// @Description  Освобождает слот игрока. Требуется авторизация.
// @Tags         game
// @Security     ApiKeyAuth
// @Produce      json
// @Param        public_key  query     string  true  "Публичный ключ игры"
// @Success      200  {object}  httpresponse.Response
// @Failure      400  {object}  httpresponse.Response
// @Failure      401  {object}  httpresponse.Response
// @Router       /leaveGame [get]
func (g *GameHandler) HandleLeaveGame(w http.ResponseWriter, r *http.Request) {
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

	ctx := r.Context()
	ok, err := g.gameUC.LeaveGame(ctx, userID)
	if err != nil || !ok {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "Пользователь успешно покинул игру")
}

// HandleJoinGame godoc
// @Summary      Присоединиться к существующей игре
// @Description  Добавляет пользователя в игру по публичному ключу. Нужна авторизация.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        payload  body      game.GameJoinRequest       true  "Ключ и роль"
// @Success      200      {object}  httpresponse.Response      "Успешно"
// @Failure      400      {object}  httpresponse.Response      "Игра не найдена / пользователь уже в игре"
// @Failure      401      {object}  httpresponse.Response
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
// @Summary      WebSocket‑сессия игры
// @Description  Апгрейд HTTP→WS для обмена ходами в режиме реального времени. После ответа 101 дальнейшее общение идёт по протоколу WebSocket.
// @Tags         game
// @Security     ApiKeyAuth
// @Produce      plain
// @Param        game_id  query     string  true  "Публичный ключ игры"
// @Success      101      {string}  string  "Переключение протокола"
// @Failure      400      {object}  httpresponse.Response
// @Failure      401      {object}  httpresponse.Response
// @Failure      403      {object}  httpresponse.Response
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
// @Summary      Получить список игр из архива с пагинацией
// @Description  Возвращает страницу архивных игр с возможностью фильтрации по году или имени игрока (один из параметров обязателен).
// @Tags         game
// @Security     ApiKeyAuth
// @Produce      json
// @Param        year     query     int     false  "Фильтр по году (если не указан фильтр по имени)"
// @Param        name     query     string  false  "Фильтр по имени игрока (если не указан год)"
// @Param        page     query     int     false  "Номер страницы (по умолчанию 0)"
// @Success      200      {object}  httpresponse.Response{Body=game.ArchiveResponse}  "Страница архивных игр"
// @Failure      400      {object}  httpresponse.Response                         "Некорректный запрос или ошибка получения архива"
// @Failure      401      {object}  httpresponse.Response                         "Неавторизованный"
// @Failure      405      {object}  httpresponse.Response                         "Метод не разрешён"
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
// @Summary      Получить список годов в архиве
// @Description  Возвращает отсортированный список лет, за которые имеются архивные игры.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Success      200      {object}  httpresponse.Response{Body=game.ArchiveYearsResponse}  "Список годов"
// @Failure      400      {object}  httpresponse.Response "Ошибка получения годов"
// @Failure      401      {object}  httpresponse.Response  "Неавторизованный"
// @Failure      405      {object}  httpresponse.Response  "Метод не разрешён"
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
// @Summary      Получить список игроков из архива
// @Description  Возвращает страницу с именами игроков, отсортированными по количеству сыгранных игр.
// @Tags         game
// @Security     ApiKeyAuth
// @Produce      json
// @Param        page     query     int     false  "Номер страницы (по умолчанию 1)"
// @Success      200      {object}  httpresponse.Response{Body=game.ArchiveNamesResponse}  "Страница имён игроков"
// @Failure      400      {object}  httpresponse.Response                              "Ошибка получения списка имён"
// @Failure      401      {object}  httpresponse.Response                              "Неавторизован"
// @Failure      405      {object}  httpresponse.Response                              "Метод не разрешён"
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
// @Summary      Получить игру из архива по идентификатору
// @Description  Возвращает запись об архивной игре по её идентификатору в архиве.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        payload  body      FindGameInArchive                               true  "Идентификатор записи архива"
// @Success      200      {object}  httpresponse.Response{Body=game.GameFromArchive}  "Данные архивной игры"
// @Failure      400      {object}  httpresponse.Response                              "Некорректный запрос или ошибка поиска игры"
// @Failure      401      {object}  httpresponse.Response                              "Неавторизован"
// @Failure      405      {object}  httpresponse.Response                              "Метод не разрешён"
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

// HandleAnalyseGame godoc
// @Summary      Анализ текущего состояния игры
// @Description  Отправляет SGF текущей игры в KataGo для анализа. Если ключ не передан, будет использована активная игра пользователя.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        secret_key query    string  false  "ключ игры (secret, опционально)"
// @Success      200      {object}  httpresponse.Response{Body=game.KataGoResponse}  "Результаты анализа от KataGo"
// @Failure      400      {object}  httpresponse.Response                              "Ошибка запроса или анализа игры"
// @Failure      401      {object}  httpresponse.Response                              "Неавторизован"
// @Failure      405      {object}  httpresponse.Response                              "Метод не разрешён"
// @Router       /analyseCurrent [get]
func (g *GameHandler) HandleAnalyseGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		g.log.Error("Разрешен только метод GET")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Разрешен только метод GET")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		g.log.Error("UserID не найден в cookie")
		httpresponse.WriteResponseWithStatus(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	ctx := r.Context()

	secretKey := r.URL.Query().Get("secret_key")
	if secretKey == "" {
		g.log.Info("Запрос на анализ игры не содержит уникальный ключ игры, будем искать в активных играх юзера")
		foundGameSecretKey, err := g.gameUC.GetActiveGameSecretKey(ctx, userID)
		if err != nil {
			g.log.Error(err)
			httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Active games not found for this user")
			return
		}
		secretKey = foundGameSecretKey
	}

	analyseResp, err := g.gameUC.AnalyseCurrentGame(ctx, secretKey)
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

// HandleGenerateMove godoc
// @Summary      Сгенерировать ход против бота
// @Description  Обрабатывает ход пользователя, генерирует ответный ход бота и возвращает обновлённый список ходов и SGF партии.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        payload  body      GenerateMoveRequest                              true  "Ход пользователя"
// @Success      200      {object}  httpresponse.Response{Body=BotGenerateMoveResponse}  "Список ходов и новый SGF"
// @Failure      400      {object}  httpresponse.Response                              "Неверный JSON или ошибка логики игры"
// @Failure      401      {object}  httpresponse.Response                              "Неавторизован"
// @Failure      405      {object}  httpresponse.Response                              "Метод не разрешён"
// @Router       /generateMove [post]
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

	resp := BotGenerateMoveResponse{
		Moves: allMoves,
		Sgf:   newSgf,
	}
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, resp)
}

// HandleNewBotGame godoc
// @Summary      Создать новую игру с ботом
// @Description  Создаёт новую игру против бота и возвращает секретный ключ игры.
// @Tags         game
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        payload  body      CreateBotGameRequest                             true  "Параметры новой игры с ботом"
// @Success      200      {object}  httpresponse.Response{Body=map[string]string}  "Секретный ключ игры"
// @Failure      400      {object}  httpresponse.Response                              "Ошибка создания игры"
// @Failure      401      {object}  httpresponse.Response                              "Неавторизован"
// @Failure      405      {object}  httpresponse.Response                              "Метод не разрешён"
// @Router       /newBotGame [post]
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
