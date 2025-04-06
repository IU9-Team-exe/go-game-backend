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
	"strconv"
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

type JsonOKResponse struct {
	Text string `json:"text"`
}

var activeGames = make(map[string]*game.Game)
var activeGamesMu sync.RWMutex

// NewGameHandler создаёт новый обработчик игр.
func NewGameHandler(cfg bootstrap.Config, log *zap.SugaredLogger, mongoAdapter *adapters.AdapterMongo, redisAdapter *adapters.AdapterRedis, authHandler *auth.AuthHandler) *GameHandler {
	return &GameHandler{
		cfg:         cfg,
		log:         log,
		gameUC:      gameuc.NewGameUseCase(repo.NewGameRepository(cfg, log, redisAdapter.GetClient(), mongoAdapter.Database)),
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
func (g *GameHandler) HandleGetGameByPublicKey(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		g.log.Error("Ошибка чтения тела запроса:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Ошибка чтения тела запроса")
		return
	}
	defer r.Body.Close()

	decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
	decoder.DisallowUnknownFields()

	var gameData game.GetGameInfoRequest
	if err = decoder.Decode(&gameData); err != nil {
		g.log.Error("Ошибка декодирования JSON:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Неверный JSON: "+err.Error())
		return
	}

	gameByID, err := g.gameUC.GetGameByPublicKey(r.Context(), gameData.GamePublicKey)
	if err != nil {
		httpresponse.WriteResponseWithStatus(w, http.StatusInternalServerError,
			httpresponse.ErrorResponse{ErrorDescription: err.Error()})
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, gameByID)
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

	if newGameRequest.BoardSize == 0 || newGameRequest.Komi == 0 {
		g.log.Error("Запрос на создание игры не содержит размер доски или коми")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Запрос не содержит размер доски или коми")
		return
	}

	userID := g.authHandler.GetUserID(w, r)
	if userID == "" {
		g.log.Error("UserID не найден в cookie")
		return
	}
	g.log.Infof("Новая игра от пользователя с id: %s", userID)

	ctx := r.Context()
	isAlreadyInGame, err := g.gameUC.HasUserActiveGamesByUserId(ctx, userID)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Ошибка проверки активных игр: "+err.Error())
		return
	}

	if isAlreadyInGame {
		g.log.Error("Пользователь уже участвует в игре")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Пользователь уже участвует в игре")
		return
	}

	err, gameKeyPublic, gameKeySecret := g.gameUC.CreateGame(ctx, newGameRequest, userID)
	if err != nil {
		g.log.Error(err)
		return
	}

	newGame, err := g.gameUC.GetGameBySecreteKey(ctx, gameKeySecret)
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
	g.log.Info("Новая игра создана с ключом: " + gameKeyPublic)
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
func (g *GameHandler) HandleJoinGame(w http.ResponseWriter, r *http.Request) {
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
	g.log.Infof("Запрос на присоединение к игре от пользователя с id: %s", userID)

	var gameJoinRequest game.GameJoinRequest
	if err := utils.DecodeJSONRequest(r, &gameJoinRequest); err != nil {
		g.log.Error("Ошибка декодирования JSON:", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	if gameJoinRequest.GameKeyPublic == "" || gameJoinRequest.Role == "" {
		g.log.Error("Запрос на присоединение к игре не содержит публичного ключа или роли")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Неверный JSON")
		return
	}

	ctx := r.Context()
	isAlreadyInGame, err := g.gameUC.HasUserActiveGamesByUserId(ctx, userID)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Ошибка проверки активных игр: "+err.Error())
		return
	}
	if isAlreadyInGame {
		g.log.Error("Пользователь уже участвует в игре")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Пользователь уже участвует в игре")
		return
	}

	play, err := g.gameUC.GetGameByPublicKey(ctx, gameJoinRequest.GameKeyPublic)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Ошибка получения игры: "+err.Error())
		return
	}

	if play.GameKeySecret == "" {
		g.log.Error("Игра не найдена! Id: " + gameJoinRequest.GameKeyPublic)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Игра не найдена! Id: "+gameJoinRequest.GameKeyPublic)
		return
	}

	play, err = g.gameUC.JoinGame(ctx, play, userID)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Ошибка при присоединении к игре: "+err.Error())
		return
	}

	activeGamesMu.Lock()
	if existingGame, ok := activeGames[play.GameKeySecret]; ok {
		if play.PlayerBlack == userID {
			existingGame.PlayerBlack = userID
		} else if play.PlayerWhite == userID {
			existingGame.PlayerWhite = userID
		}
	} else {
		retrievedGame, err := g.gameUC.GetGameByPublicKey(ctx, gameJoinRequest.GameKeyPublic)
		if err == nil {
			activeGames[play.GameKeySecret] = &retrievedGame
		}
	}
	activeGamesMu.Unlock()

	resp := JsonOKResponse{
		Text: "Пользователь успешно присоединился",
	}
	httpresponse.WriteResponseWithStatus(w, http.StatusOK, resp)
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
func (g *GameHandler) HandleStartGame(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	gameID := r.URL.Query().Get("game_id")
	playerID := g.authHandler.GetUserID(w, r)
	if gameID == "" || playerID == "" {
		g.log.Error("Отсутствует gameID или playerID")
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, "Отсутствует gameID или playerID")
		return
	}
	g.log.Infof("ID игрока: %s", playerID)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Ошибка при апгрейде до WebSocket:", err)
		return
	}

	retrievedGame, err := g.gameUC.GetGameByPublicKey(ctx, gameID)
	if err != nil {
		g.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	activeGamesMu.Lock()
	ag, ok := activeGames[retrievedGame.GameKeySecret]
	if !ok {
		activeGames[retrievedGame.GameKeySecret] = &retrievedGame
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
		g.log.Error("Неизвестный ID игрока:", playerID)
		fmt.Println(ag)
		conn.Close()
		return
	}

	if *playerWS != nil {
		(*playerWS).WriteMessage(websocket.TextMessage, []byte("Вы были отключены, создано новое соединение."))
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
			g.log.Error("Ошибка чтения JSON из WebSocket:", err)
			return
		}
		g.log.Info("Получен ход:", move)
		sgfString, err := g.gameUC.AddMoveToGameSgf(ag.GameKeySecret, move)
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
				g.log.Error("Ошибка отправки сообщения оппоненту:", err)
				(*opponentWS).Close()
				activeGamesMu.Lock()
				*opponentWS = nil
				activeGamesMu.Unlock()
			}
		} else {
			conn.WriteMessage(websocket.TextMessage, []byte("Оппонент не подключён"))
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
