package task

// Task represents a single go problem pulled from an SGF file.
// swagger:model Task
type Task struct {
	// Unique identifier for this task
	// required: true
	// example: 42
	TaskUniqNumber int `json:"task_number" bson:"task_number"`

	// Difficulty level (chapter index) of the task
	// required: true
	// example: 3
	TaskLevel int `json:"task_level" bson:"task_level"`

	// Raw SGF content of the problem
	// required: true
	// example: "(;SZ[19]... )"
	TaskSgf string `json:"task_sgf" bson:"task_sgf"`

	// Current status, one of "done" or "not_done"
	// required: true
	// example: "not_done"
	TaskStatus string `json:"task_status" bson:"task_status"`
}

// TaskResponse is the paginated response wrapper for a list of tasks.
// swagger:model TaskResponse
type TaskResponse struct {
	// Current page number (1-based)
	// required: true
	// example: 1
	PageNum int `json:"page_tmp" bson:"page_tmp"`

	// Total number of pages available
	// required: true
	// example: 5
	TotalPages int `json:"total_pages" bson:"total_pages"`

	// First page that still contains not_done tasks
	// required: true
	// example: 1
	PageWithUnresolved int `json:"page_with_unresolved" bson:"page_with_unresolved"`

	// Tasks on this page
	// required: true
	Tasks []Task `json:"tasks" bson:"tasks"`
}
