package tasks

import (
	"context"
	"team_exe/internal/domain/task"
)

type TaskStore interface {
	PutAllTasksToMongoByPath(ctx context.Context, pathToTasks string) error
	GetTasksWithStatusPaginated(ctx context.Context, userIDStr string, taskLevel int, pageNum int) (*task.TaskResponse, error)
	TaskIsDone(ctx context.Context, taskUniqNumber int, userID string) (bool, error)
}

type TaskUseCase struct {
	taskStore TaskStore
}

func NewTaskUseCase(taskStore TaskStore) *TaskUseCase {
	return &TaskUseCase{taskStore: taskStore}
}

func (t *TaskUseCase) PutTasksToMongoByPath(path string) error {
	return t.taskStore.PutAllTasksToMongoByPath(context.Background(), path)
}

func (t *TaskUseCase) GetAvailableTasksForUserByIdByLevelByPage(ctx context.Context, userID string, pageNum int, level int) (*task.TaskResponse, error) {
	return t.taskStore.GetTasksWithStatusPaginated(ctx, userID, level, pageNum)
}

func (t *TaskUseCase) MarkTaskAsDone(ctx context.Context, userID string, taskID int) error {
	_, err := t.taskStore.TaskIsDone(ctx, taskID, userID)
	return err
}
