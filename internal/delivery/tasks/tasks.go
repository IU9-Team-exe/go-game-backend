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
// @Summary      Store tasks in MongoDB
// @Description  Reads all `.sgf` files from the given path and saves them into the MongoDB `tasks` collection.
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        path  query     string  true  "Filesystem path to directory containing SGF files"
// @Success      200   {string}  string  "Successfully stored tasks in MongoDB"
// @Failure      405   {string}  string  "Method Not Allowed"
// @Failure      416   {string}  string  "Requested Range Not Satisfiable"  // e.g. error during file processing or Mongo insert
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
// @Summary      Get available tasks for a user
// @Description  Returns a paginated list of tasks at the specified difficulty level, marking each as done or not_done.
// @Tags         tasks
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        page   query     int     true   "Page number (1-based)"
// @Param        level  query     int     true   "Task difficulty level"
// @Success      200    {object}  task.TaskResponse
// @Failure      401    {string}  string  "Unauthorized"                     // if no valid user cookie
// @Failure      405    {string}  string  "Method Not Allowed"
// @Failure      416    {string}  string  "Requested Range Not Satisfiable"  // e.g. missing or invalid parameters
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
// @Summary      Mark a task as done for the current user
// @Description  Marks the task with the given ID as completed in the user's record.
// @Tags         tasks
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        taskID  query     int     true   "Unique identifier of the task to mark as done"
// @Success      200     {string}  string  "ok"
// @Failure      401     {string}  string  "Unauthorized"                     // if no valid user cookie
// @Failure      405     {string}  string  "Method Not Allowed"
// @Failure      416     {string}  string  "Requested Range Not Satisfiable"  // e.g. missing or invalid taskID
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
