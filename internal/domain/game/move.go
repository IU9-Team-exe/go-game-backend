package game

type Move struct {
	Color       string `json:"color"`
	Coordinates string `json:"coordinates"`
}

type MovePSV struct {
	Move string `json:"move"`
	PSV  int    `json:"psv"`
}

type Diagnostics struct {
	BestTen []MovePSV `json:"best_ten"`
	BotMove string    `json:"bot_move"`
	Score   float64   `json:"score"`
	WinProb float64   `json:"winprob"`
}

type BotResponse struct {
	BotMove     string      `json:"bot_move"`
	Diagnostics Diagnostics `json:"diagnostics"`
	RequestID   string      `json:"request_id"`
}

type Moves struct {
	Moves []Move `json:"moves"`
}
