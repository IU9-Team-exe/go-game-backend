package tasks

import (
	"fmt"
	"go.uber.org/zap"
	"net/http"
	"strconv"
	"team_exe/internal/adapters"
	"team_exe/internal/bootstrap"
	"team_exe/internal/delivery/auth"
	"team_exe/internal/httpresponse"
	"team_exe/internal/repository"
	"team_exe/internal/usecase/tasks"
)

type TaskHandler struct {
	log         *zap.SugaredLogger
	taskUC      *tasks.TaskUseCase
	authHandler *auth.AuthHandler
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
		th.log.Error("Разрешен только метод GET")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Разрешен только метод GET")
		return
	}

	taskPath := r.URL.Query().Get("path")
	err := th.taskUC.PutTasksToMongoByPath(taskPath)
	if err != nil {
		th.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusRequestedRangeNotSatisfiable, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "Успешно положили в монгу")
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
		th.log.Error("Разрешен только метод GET")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Разрешен только метод GET")
		return
	}

	pageNum := r.URL.Query().Get("page")

	if pageNum == "" {
		err := fmt.Errorf("не указан в параметрах номер страницы")
		th.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusRequestedRangeNotSatisfiable, err)
		return
	}

	pageNumInt, err := strconv.Atoi(pageNum)
	if err != nil {
		th.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusRequestedRangeNotSatisfiable, err.Error())
		return
	}

	level := r.URL.Query().Get("level")
	if level == "" {
		err = fmt.Errorf("не указан в параметрах level задачи")
		th.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusRequestedRangeNotSatisfiable, err)
		return
	}

	levelInt, err := strconv.Atoi(level)
	if err != nil {
		th.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusRequestedRangeNotSatisfiable, err.Error())
		return
	}

	ctx := r.Context()

	userID := th.authHandler.GetUserID(w, r)
	if userID == "" {
		th.log.Error("UserID не найден в cookie")
		httpresponse.WriteResponseWithStatus(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	taskResponse, err := th.taskUC.GetAvailableTasksForUserByIdByLevelByPage(ctx, userID, pageNumInt, levelInt)

	if err != nil {
		th.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusRequestedRangeNotSatisfiable, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, taskResponse)
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
		th.log.Error("Разрешен только метод GET")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Разрешен только метод GET")
		return
	}

	userID := th.authHandler.GetUserID(w, r)
	if userID == "" {
		th.log.Error("UserID не найден в cookie")
		httpresponse.WriteResponseWithStatus(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	taskID := r.URL.Query().Get("taskID")
	if taskID == "" {
		err := fmt.Errorf("не указан в параметрах id задачи")
		th.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusRequestedRangeNotSatisfiable, err)
		return
	}

	taskInt, err := strconv.Atoi(taskID)
	if err != nil {
		th.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusRequestedRangeNotSatisfiable, err.Error())
		return
	}

	ctx := r.Context()

	err = th.taskUC.MarkTaskAsDone(ctx, userID, taskInt)
	if err != nil {
		th.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusRequestedRangeNotSatisfiable, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "ok")
}
