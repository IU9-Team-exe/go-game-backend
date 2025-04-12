package task

type Task struct {
	TaskUniqNumber int    `json:"task_number" bson:"task_number"`
	TaskLevel      int    `json:"task_level" bson:"task_level"`
	TaskSgf        string `json:"task_sgf" bson:"task_sgf"`
	TaskStatus     string `json:"task_status" bson:"task_status"`
}

type TaskResponse struct {
	PageNum            int    `json:"page_tmp" bson:"page_tmp"`
	TotalPages         int    `json:"total_pages" bson:"total_pages"`
	PageWithUnresolved int    `json:"page_with_unresolved" bson:"page_with_unresolved"`
	Tasks              []Task `json:"tasks" bson:"tasks"`
}
