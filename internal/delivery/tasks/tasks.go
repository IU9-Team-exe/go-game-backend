package tasks

import (
	"go.uber.org/zap"
	"net/http"
	"strconv"
	"team_exe/internal/adapters"
	"team_exe/internal/bootstrap"
	"team_exe/internal/httpresponse"
	"team_exe/internal/repository"
	"team_exe/internal/usecase/tasks"
)

type TaskHandler struct {
	log    *zap.SugaredLogger
	taskUC *tasks.TaskUseCase
}

func NewTaskHandler(log *zap.SugaredLogger, cfg *bootstrap.Config, mongoAdapter *adapters.AdapterMongo) *TaskHandler {
	return &TaskHandler{
		taskUC: tasks.NewTaskUseCase(repository.NewTaskStorage(cfg, mongoAdapter)),
		log:    log,
	}
}

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

func (th *TaskHandler) HandleGetAvailableGamesForUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		th.log.Error("Разрешен только метод GET")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Разрешен только метод GET")
		return
	}

	pageNum := r.URL.Query().Get("page")
	pageNumInt, err := strconv.Atoi(pageNum)
	if err != nil {
		th.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusRequestedRangeNotSatisfiable, err.Error())
		return
	}

	userID := r.URL.Query().Get("page")

	level := r.URL.Query().Get("level")
	levelInt, err := strconv.Atoi(level)
	if err != nil {
		th.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusRequestedRangeNotSatisfiable, err.Error())
		return
	}

	ctx := r.Context()

	taskResponse, err := th.taskUC.GetAvailableTasksForUserByIdByLevelByPage(ctx, userID, pageNumInt, levelInt)

	if err != nil {
		th.log.Error(err)
		httpresponse.WriteResponseWithStatus(w, http.StatusRequestedRangeNotSatisfiable, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, taskResponse)
}
