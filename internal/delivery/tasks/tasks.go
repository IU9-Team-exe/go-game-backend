package tasks

import (
	"go.uber.org/zap"
	"net/http"
	"strconv"
	"team_exe/internal/adapters"
	"team_exe/internal/bootstrap"
	"team_exe/internal/delivery/auth"
	errs "team_exe/internal/errors"
	"team_exe/internal/httpresponse"
	"team_exe/internal/repository"
	"team_exe/internal/usecase/tasks"
)

type TaskHandler struct {
	log         *zap.SugaredLogger
	taskUC      *tasks.TaskUseCase
	authHandler *auth.AuthHandler
}

type JsonOKResponse struct {
	Text string `json:"text"`
}

func NewTaskHandler(log *zap.SugaredLogger, cfg *bootstrap.Config, auth *auth.AuthHandler, mongoAdapter *adapters.AdapterMongo) *TaskHandler {
	return &TaskHandler{
		taskUC:      tasks.NewTaskUseCase(repository.NewTaskStorage(cfg, mongoAdapter)),
		log:         log,
		authHandler: auth,
	}
}

// HandleStoreInMongo godoc
// @Summary      Загрузить задачи SGF в MongoDB
// @Description  Рекурсивно читает все файлы `*.sgf` по указанному пути и сохраняет их в коллекцию `tasks`.
// @Tags         tasks
// @Produce      json
// @Param        path  query     string  true  "Путь к директории с SGF"
// @Success      200   {object}  httpresponse.Response      "ОК"
// @Failure      405   {object}  httpresponse.Response      "Метод не поддерживается"
// @Failure      416   {object}  httpresponse.Response      "Ошибка чтения файлов"
// @Router       /storeTasksToMongoByPath [get]
func (th *TaskHandler) HandleStoreInMongo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only Get allowed")
		return
	}

	taskPath := r.URL.Query().Get("path")
	err := th.taskUC.PutTasksToMongoByPath(taskPath)
	if err != nil {
		status, code := errs.TranslateErr(err)
		httpresponse.WriteAPIError(w, status, code, "Error while put tasks to mongo: "+err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "successfully put tasks to mongo")
}

// HandleGetAvailableGamesForUser godoc
// @Summary      Список задач пользователя
// @Description  Возвращает постраничный список задач указанного уровня сложности с пометкой «решена/нет».
// @Tags         tasks
// @Security     ApiKeyAuth
// @Produce      json
// @Param        page   query     int  true  "Номер страницы (1‑based)"
// @Param        level  query     int  true  "Уровень сложности"
// @Success      200    {object}  task.TaskResponse
// @Failure      401    {object}  httpresponse.Response
// @Failure      416    {object}  httpresponse.Response      "Некорректные параметры"
// @Router       /getAvailableGamesForUser [get]
func (th *TaskHandler) HandleGetAvailableGamesForUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		th.log.Error("HandleGetAvailableGamesForUser: only GET allowed")
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET allowed")
		return
	}

	pageStr := r.URL.Query().Get("page")
	if pageStr == "" {
		th.log.Error("HandleGetAvailableGamesForUser: missing page parameter")
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "missing_page", "page parameter is required")
		return
	}
	page, err := strconv.Atoi(pageStr)
	if err != nil {
		th.log.Error("HandleGetAvailableGamesForUser: invalid page:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "invalid_page", err.Error())
		return
	}

	levelStr := r.URL.Query().Get("level")
	if levelStr == "" {
		th.log.Error("HandleGetAvailableGamesForUser: missing level parameter")
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "missing_level", "level parameter is required")
		return
	}
	level, err := strconv.Atoi(levelStr)
	if err != nil {
		th.log.Error("HandleGetAvailableGamesForUser: invalid level:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "invalid_level", err.Error())
		return
	}

	userID := th.authHandler.GetUserID(w, r)
	if userID == "" {
		th.log.Error("HandleGetAvailableGamesForUser: unauthorized")
		httpresponse.WriteAPIError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	resp, err := th.taskUC.GetAvailableTasksForUserByIdByLevelByPage(r.Context(), userID, page, level)
	if err != nil {
		th.log.Error("HandleGetAvailableGamesForUser:", err)
		status, code := errs.TranslateErr(err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, resp)
}

// HandleMarkTaskAsDone godoc
// @Summary      Отметить задачу как решённую
// @Description  Добавляет ID задачи в список выполненных пользователя.
// @Tags         tasks
// @Security     ApiKeyAuth
// @Produce      json
// @Param        taskID  query     int  true  "ID задачи"
// @Success      200     {object}  httpresponse.Response
// @Failure      401     {object}  httpresponse.Response
// @Failure      416     {object}  httpresponse.Response      "Некорректный taskID"
// @Router       /markTaskAsDone [get]
func (th *TaskHandler) HandleMarkTaskAsDone(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		th.log.Error("HandleMarkTaskAsDone: only GET allowed")
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET allowed")
		return
	}

	userID := th.authHandler.GetUserID(w, r)
	if userID == "" {
		th.log.Error("HandleMarkTaskAsDone: unauthorized")
		httpresponse.WriteAPIError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized")
		return
	}

	idStr := r.URL.Query().Get("taskID")
	if idStr == "" {
		th.log.Error("HandleMarkTaskAsDone: missing taskID parameter")
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "missing_task_id", "taskID parameter is required")
		return
	}
	taskID, err := strconv.Atoi(idStr)
	if err != nil {
		th.log.Error("HandleMarkTaskAsDone: invalid taskID:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "invalid_task_id", err.Error())
		return
	}

	if err := th.taskUC.MarkTaskAsDone(r.Context(), userID, taskID); err != nil {
		th.log.Error("HandleMarkTaskAsDone:", err)
		status, code := errs.TranslateErr(err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, JsonOKResponse{Text: "ok"})
}
