package domain

type AnalysisRequest struct {
	ID               string      `json:"id"`
	Moves            [][2]string `json:"moves"` // [["b","D4"], ["w","Q16"], ...]
	Rules            string      `json:"rules"`
	Komi             float64     `json:"komi"`
	BoardXSize       int         `json:"boardXSize"`
	BoardYSize       int         `json:"boardYSize"`
	MaxVisits        int         `json:"maxVisits,omitempty"`
	IncludeOwnership bool        `json:"includeOwnership,omitempty"`
}

// Ответ KataGo с анализом позиции
type AnalysisResponse struct {
	ID             string     `json:"id"`
	TurnNumber     int        `json:"turnNumber"`
	IsDuringSearch bool       `json:"isDuringSearch"`
	RootInfo       RootInfo   `json:"rootInfo"`
	MoveInfos      []MoveInfo `json:"moveInfos"`
}

// Информация о корневой позиции (общая информация)
type RootInfo struct {
	CurrentPlayer string  `json:"currentPlayer"` // "W" или "B"
	Winrate       float64 `json:"winrate"`
	ScoreLead     float64 `json:"scoreLead"`
	ScoreSelfplay float64 `json:"scoreSelfplay"`
	ScoreStdev    float64 `json:"scoreStdev"`
	Utility       float64 `json:"utility"`
	Visits        int     `json:"visits"`
}

// Информация о возможных ходах (вариантах)
type MoveInfo struct {
	Move      string   `json:"move"`
	Winrate   float64  `json:"winrate"`
	Visits    int      `json:"visits"`
	ScoreLead float64  `json:"scoreLead"`
	PV        []string `json:"pv"` // Principal Variation (последовательность ходов)
}
