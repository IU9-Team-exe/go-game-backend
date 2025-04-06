package game

// @name Move
type Move struct {
	Color       string `json:"color"`
	Coordinates string `json:"coordinates"`
}

// @name MovePSV
type MovePSV struct {
	Move string `json:"move"`
	PSV  int    `json:"psv"`
}

// @name Diagnostics
type Diagnostics struct {
	BestTen []MovePSV `json:"best_ten"`
	BotMove string    `json:"bot_move"`
	Score   float64   `json:"score"`
	WinProb float64   `json:"winprob"`
}

// @name BotResponse
type BotResponse struct {
	BotMove     string      `json:"bot_move"`
	Diagnostics Diagnostics `json:"diagnostics"`
	RequestID   string      `json:"request_id"`
}

// @name Moves
type Moves struct {
	Moves []Move `json:"moves"`
}
