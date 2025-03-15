package repository

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"os/exec"
	"sync"

	"team_exe/internal/adapters"
	"team_exe/internal/bootstrap"
	"team_exe/internal/domain"
)

// -----------------------------------------------------
// Пример структур для запросов/ответов к KataGo
// -----------------------------------------------------

// KatagoRequest — то, что мы шлём в KataGo на вход (в stdout процесса).
// Поля можете адаптировать под свой JSON-протокол.
type KatagoRequest struct {
	ID        string `json:"id"`        // Уникальный ID запроса, чтобы KataGo вернул ответ с тем же ID
	Cmd       string `json:"cmd"`       // Команда, которую KataGo должен выполнить (например, "play", "genmove", "estimate_score" и т.д.)
	GameID    string `json:"gameId"`    // ID игры, чтобы KataGo мог понимать, о какой партии речь (если он поддерживает многопартийность)
	Move      string `json:"move"`      // Собственно ход (например, "dp" или "Q16"), если нужно
	Color     string `json:"color"`     // "B" или "W", если нужно
	SGF       string `json:"sgf"`       // Текущее SGF, если вы хотите передавать всю историю партии
	MaxVisits int    `json:"maxVisits"` // Для ограничений анализа (опционально)
}

// KatagoResponse — ответ, который приходит от KataGo (из stdin процесса).
// Тоже примерная структура – подгоните под ваш формат.
type KatagoResponse struct {
	ID    string  `json:"id"`        // Должен совпадать с KatagoRequest.ID
	Legal bool    `json:"legalMove"` // Легален ли ход
	Score float64 `json:"score"`     // Какой-то счёт (или разница, или winrate)
	Error string  `json:"error"`     // Может содержать текст ошибки, если ход нелегален
	// Любые другие поля, которые вы возвращаете
}

// -----------------------------------------------------
// Структуры KatagoRepository, KatagoClient
// -----------------------------------------------------

type KatagoRepository struct {
	cfg    *bootstrap.Config
	log    *zap.SugaredLogger
	client *KatagoClient
	redis  *adapters.AdapterRedis
	mongo  *adapters.AdapterMongo
}

// KatagoClient управляет процессом KataGo: пишет ему в stdin, читает из stdout.
type KatagoClient struct {
	cmd      *exec.Cmd
	stdin    *bufio.Writer
	stdout   *bufio.Scanner
	mu       sync.Mutex
	response sync.Map // map[requestID]chan KatagoResponse
	log      *zap.SugaredLogger
}

// -----------------------------------------------------
// Конструкторы
// -----------------------------------------------------

func NewKatagoRepository(cfg *bootstrap.Config, log *zap.SugaredLogger) (*KatagoRepository, error) {
	client, err := NewKatagoClient(log)
	if err != nil {
		return nil, err
	}

	return &KatagoRepository{
		cfg:    cfg,
		log:    log,
		client: client,
		mongo:  adapters.NewAdapterMongo(cfg),
		redis:  adapters.NewAdapterRedis(cfg),
	}, nil
}

func NewKatagoClient(log *zap.SugaredLogger) (*KatagoClient, error) {
	// Запускаем KataGo с нужными параметрами
	cmd := exec.Command(
		"./katago",
		"analysis",
		"-model",
		"kata1-b40c256-s11840935168-d2898845681.bin",
		"-config",
		"gtp_custom.cfg",
	)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	client := &KatagoClient{
		cmd:    cmd,
		stdin:  bufio.NewWriter(stdinPipe),
		stdout: bufio.NewScanner(stdoutPipe),
		log:    log,
	}

	// Стартуем процесс KataGo
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Запускаем горутину чтения stdout KataGo
	go client.listenForResponses()

	return client, nil
}

// -----------------------------------------------------
// Асинхронная логика отправки запроса KataGo и ожидания ответа
// -----------------------------------------------------

func (c *KatagoClient) listenForResponses() {
	for c.stdout.Scan() {
		line := c.stdout.Text()

		var resp KatagoResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			c.log.Errorw("failed to unmarshal KataGo response", "error", err, "line", line)
			continue
		}

		// Находим канал по resp.ID
		if chIface, ok := c.response.Load(resp.ID); ok {
			ch := chIface.(chan KatagoResponse)
			ch <- resp
			c.response.Delete(resp.ID)
		} else {
			c.log.Warnw("no channel found for response ID", "id", resp.ID)
		}
	}
}

func (c *KatagoClient) Analyze(request KatagoRequest) (KatagoResponse, error) {
	// Канал для ответа
	responseChan := make(chan KatagoResponse, 1)

	// Сохраняем канал для этого request.ID
	c.response.Store(request.ID, responseChan)

	// Преобразуем запрос в JSON-строку
	requestJSON, err := json.Marshal(request)
	if err != nil {
		c.response.Delete(request.ID)
		return KatagoResponse{}, err
	}

	// Пишем в stdin KataGo (защищаем мьютексом, чтобы не перемешать запросы)
	c.mu.Lock()
	_, err = c.stdin.Write(append(requestJSON, '\n'))
	if err != nil {
		c.mu.Unlock()
		c.response.Delete(request.ID)
		return KatagoResponse{}, err
	}
	c.stdin.Flush()
	c.mu.Unlock()

	// Ждём ответ
	// Можно добавить select с таймаутом, если необходимо
	resp := <-responseChan
	return resp, nil
}

// -----------------------------------------------------
// Утилита
// -----------------------------------------------------

func GenerateUuid() string {
	return uuid.New().String()
}

// -----------------------------------------------------
// Методы StartGame, MakeMove, EndGame
// -----------------------------------------------------

// StartGame инициализирует новую игру
func (k *KatagoRepository) StartGame(ctx context.Context, req domain.KatagoGameStartRequest) (string, error) {
	gameID := GenerateUuid()

	// Создадим базовый SGF (примерно)
	sz := req.BoardXSize
	if sz == 0 {
		sz = 19 // по умолчанию
	}
	komi := req.Komi
	if komi == 0 {
		komi = 6.5
	}
	initialSGF := fmt.Sprintf("(;FF[4]SZ[%d]KM[%f])", sz, komi)

	// Сохраняем в Redis
	err := k.storeGameSGF(ctx, gameID, initialSGF)
	if err != nil {
		k.log.Errorw("failed to store initial SGF", "error", err)
		return "", err
	}

	// Можно (не обязательно) сделать начальный вызов KataGo,
	// чтобы "зарегистрировать" партию, если у вас так задумано:
	if req.MaxVisits > 0 {
		initReq := KatagoRequest{
			ID:        GenerateUuid(),
			Cmd:       "initGame", // Допустим, у вас есть такая команда
			GameID:    gameID,
			SGF:       initialSGF,
			MaxVisits: req.MaxVisits,
		}
		if resp, err := k.client.Analyze(initReq); err != nil {
			k.log.Errorw("failed to analyze initial position", "error", err)
		} else {
			k.log.Infof("Initial KataGo response: %+v", resp)
		}
	}

	return gameID, nil
}

// MakeMove добавляет ход в SGF, проверяет легальность, возвращает счёт (score) и обновлённый SGF
func (k *KatagoRepository) MakeMove(ctx context.Context, gameID string, move domain.KatagoMoveRequest) (updatedSGF string, score float64, err error) {
	// Достаём текущее SGF
	currentSGF, err := k.getGameSGF(ctx, gameID)
	if err != nil {
		return "", 0, err
	}

	// Формируем следующий ход в SGF (например ";B[dp]")
	nextMoveSgf := fmt.Sprintf(";%s[%s]", move.Color, move.Move)
	// Пока упрощённо: вставим перед финальной ')'
	newSGF := insertMoveInSGF(currentSGF, nextMoveSgf)

	// Отправляем запрос в KataGo, чтобы проверить, легален ли ход, и узнать счёт
	req := KatagoRequest{
		ID:     GenerateUuid(),
		Cmd:    "play", // или любая ваша команда, которая проверит ход
		GameID: gameID,
		Move:   move.Move,
		Color:  move.Color,
		SGF:    currentSGF, // передаём старое SGF или всё целиком
	}

	resp, err := k.client.Analyze(req)
	if err != nil {
		// Не удалось получить ответ от KataGo
		return "", 0, err
	}

	if !resp.Legal {
		// Ход нелегален, возвращаем ошибку
		return "", 0, errors.New("illegal move: " + resp.Error)
	}

	// Если дошли сюда, значит ход легален
	// Сохраняем обновлённое SGF в Redis
	err = k.storeGameSGF(ctx, gameID, newSGF)
	if err != nil {
		return "", 0, err
	}

	// Из ответа берем, например, score (если KataGo так возвращает)
	return newSGF, resp.Score, nil
}

// EndGame завершает игру, убираем из Redis
func (k *KatagoRepository) EndGame(ctx context.Context, gameID string) error {
	redisClient := k.redis.GetClient()
	err := redisClient.Del(ctx, gameID).Err()
	if err != nil {
		k.log.Errorw("failed to delete game from redis", "gameID", gameID, "error", err)
		return err
	}

	// Опционально можно послать команду KataGo "quitGame" или что-то подобное,
	// если нужно, чтобы KataGo сбросил контекст
	// request := KatagoRequest{
	//    ID:    GenerateUuid(),
	//    Cmd:   "quitGame",
	//    GameID: gameID,
	// }
	// _, _ = k.client.Analyze(request)

	return nil
}

// -----------------------------------------------------
// Работа с Redis, SGF и вспомогательные функции
// -----------------------------------------------------

func (k *KatagoRepository) storeGameSGF(ctx context.Context, gameID, sgf string) error {
	data := map[string]string{
		"sgf": sgf,
	}
	bytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	redisClient := k.redis.GetClient()
	return redisClient.Set(ctx, gameID, bytes, 0).Err()
}

func (k *KatagoRepository) getGameSGF(ctx context.Context, gameID string) (string, error) {
	redisClient := k.redis.GetClient()
	val, err := redisClient.Get(ctx, gameID).Result()
	if err != nil {
		return "", err
	}

	var data struct {
		SGF string `json:"sgf"`
	}
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return "", err
	}

	return data.SGF, nil
}

// insertMoveInSGF просто вставляет ход перед финальной ')'
// В реальном проекте лучше пользоваться полноценным SGF-парсером
func insertMoveInSGF(currentSGF, move string) string {
	if len(currentSGF) == 0 {
		return "(" + move + ")"
	}
	pos := -1
	for i := len(currentSGF) - 1; i >= 0; i-- {
		if currentSGF[i] == ')' {
			pos = i
			break
		}
	}
	if pos == -1 {
		// Кривой SGF
		return currentSGF + move
	}
	return currentSGF[:pos] + move + currentSGF[pos:]
}
