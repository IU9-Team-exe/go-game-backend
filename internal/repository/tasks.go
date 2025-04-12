package repository

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"team_exe/internal/adapters"
	"team_exe/internal/bootstrap"
	"team_exe/internal/domain/task"
)

type TaskStorage struct {
	cfg   *bootstrap.Config
	mongo *adapters.AdapterMongo
}

func NewTaskStorage(cfg *bootstrap.Config, mongoAdapter *adapters.AdapterMongo) *TaskStorage {
	return &TaskStorage{
		cfg:   cfg,
		mongo: mongoAdapter,
	}
}

func (t *TaskStorage) PutAllTasksToMongoByPath(ctx context.Context, pathToTasks string) error {
	err := filepath.Walk(pathToTasks, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if strings.HasSuffix(info.Name(), ".sgf") {
			taskStruct, err := t.ConvertSgfTaskToStructTask(path)
			if err != nil {
				return fmt.Errorf("Ошибка при обработке файла %s: %v\n", path, err)
			}

			err = t.SaveToMongo(ctx, taskStruct)
			if err != nil {
				return fmt.Errorf("Ошибка при сохранении в Mongo %s: %v\n", path, err)
			}
		}

		return nil
	})

	return err
}

func (t *TaskStorage) ConvertSgfTaskToStructTask(pathToTask string) (*task.Task, error) {
	filename := filepath.Base(pathToTask)
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	taskUniqNum, err := strconv.Atoi(name)
	if err != nil {
		return nil, err
	}
	taskLevel, ok := ExtractChapterIndex(pathToTask)
	if !ok {
		taskLevel = 0
	}

	data, err := os.ReadFile(pathToTask)
	if err != nil {
		return nil, err
	}

	processedTask := &task.Task{
		TaskUniqNumber: taskUniqNum,
		TaskLevel:      taskLevel,
		TaskSgf:        string(data),
	}

	return processedTask, nil
}

func ExtractChapterIndex(pathToTask string) (int, bool) {
	dirs := strings.Split(filepath.ToSlash(pathToTask), "/")

	re := regexp.MustCompile(`(?i)^Chapter (\d+)$`)

	for _, dir := range dirs {
		if match := re.FindStringSubmatch(dir); len(match) == 2 {
			indexNum, err := strconv.Atoi(match[1])
			if err != nil {
				fmt.Println(err)
				return 0, false
			}
			return indexNum, true
		}
	}
	fmt.Println("что то странное с ExtractChapterIndex")
	return 0, false
}

func (t *TaskStorage) SaveToMongo(ctx context.Context, task *task.Task) error {
	_, err := t.mongo.Database.Collection("tasks").InsertOne(ctx, task)
	return err
}
func (t *TaskStorage) GetTasksWithStatusPaginated(
	ctx context.Context,
	userIDStr string,
	taskLevel int,
	pageNum int,
) (*task.TaskResponse, error) {

	var user struct {
		TasksDone []int `bson:"done_tasks_ids"` // task_number!
	}

	err := t.mongo.Database.Collection("users").
		FindOne(ctx, bson.M{"_id": userIDStr}). // <--- здесь используем строку
		Decode(&user)

	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	filter := bson.M{"task_level": taskLevel}
	cursor, err := t.mongo.Database.Collection("tasks").Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var allTasks []task.Task
	if err := cursor.All(ctx, &allTasks); err != nil {
		return nil, err
	}

	// 3. Отметим done/not_done
	doneMap := make(map[int]struct{}, len(user.TasksDone))
	for _, n := range user.TasksDone {
		doneMap[n] = struct{}{}
	}

	for i := range allTasks {
		if _, ok := doneMap[allTasks[i].TaskUniqNumber]; ok {
			allTasks[i].TaskStatus = "done"
		} else {
			allTasks[i].TaskStatus = "not_done"
		}
	}

	// 4. Сортируем: done → not_done
	sort.SliceStable(allTasks, func(i, j int) bool {
		return allTasks[i].TaskStatus < allTasks[j].TaskStatus // "done" < "not_done"
	})

	// 5. Определяем, на какой странице начинаются not_done
	pageWithUnresolved := 0
	pageLimit := t.cfg.PageLimitTasks
	for i, task := range allTasks {
		if task.TaskStatus == "not_done" {
			pageWithUnresolved = i / pageLimit
			break
		}
	}

	// 6. Пагинация
	totalPages := (len(allTasks) + pageLimit - 1) / pageLimit
	start := pageNum * pageLimit
	end := start + pageLimit
	if start > len(allTasks) {
		start = len(allTasks)
	}
	if end > len(allTasks) {
		end = len(allTasks)
	}

	// 7. Ответ
	return &task.TaskResponse{
		PageNum:            pageNum,
		TotalPages:         totalPages,
		PageWithUnresolved: pageWithUnresolved,
		Tasks:              allTasks[start:end],
	}, nil
}
