package domain

type KatagoMoveRequest struct {
	GameId string `json:"game_id"`
	Move   string `json:"move"`
	Color  string `json:"color"`
}

type KatagoMoveResponse struct {
	GameId string             `json:"game_id"`
	SGF    string             `json:"board_state"`
	Scores map[string]float64 `json:"scores"`
}

type KatagoGameStartRequest struct {
	Rules         string        `json:"rules"`
	Komi          float64       `json:"komi"`
	BoardXSize    int           `json:"board_X_size"`
	BoardYSize    int           `json:"board_Y_size"`
	InitialStones []interface{} `json:"initial_stones"`
	TimeLimit     float64       `json:"time_limit"`
	AnalyzeTurns  []int         `json:"analyze_turns"`
	PlayersIds    []string      `json:"players_ids"`
	MaxVisits     int           `json:"max_visits"`
	GameId        string        `json:"game_id"`
}

type KatagoGameStartResponse struct {
	GameId string `json:"game_id"`
}

type KatagoRequest struct {
	GameId  string `json:"game_id"`
	MoveSGF string `json:"move"`
}

type Move struct {
	Color       string `json:"color"`
	Coordinates string `json:"coordinates"`
}
