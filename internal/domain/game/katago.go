package game

type KatagoRequest struct {
	Id               string     `json:"id"`
	Rules            string     `json:"rules"`
	BoardXSize       int        `json:"boardXSize"`
	BoardYSize       int        `json:"boardYSize"`
	Moves            [][]string `json:"moves"`
	IncludeOwnership bool       `json:"includeOwnership"`
	IncludePolicy    bool       `json:"includePolicy"`
}

type KataGoResponse struct {
	ID             string     `json:"id,omitempty"`
	IsDuringSearch bool       `json:"isDuringSearch,omitempty"`
	MoveInfos      []MoveInfo `json:"moveInfos,omitempty"`
	RootInfo       RootInfo   `json:"rootInfo,omitempty"`
	TurnNumber     int        `json:"turnNumber,omitempty"`
	Ownership      []float64  `json:"ownership,omitempty"`
	Policy         []float64  `json:"policy,omitempty"`
	Error          string     `json:"error,omitempty"`
}

type MoveInfo struct {
	EdgeVisits    int      `json:"edgeVisits"`
	EdgeWeight    float64  `json:"edgeWeight"`
	LCB           float64  `json:"lcb"`
	Move          string   `json:"move"`
	Order         int      `json:"order"`
	Prior         float64  `json:"prior"`
	PV            []string `json:"pv"`
	ScoreLead     float64  `json:"scoreLead"`
	ScoreMean     float64  `json:"scoreMean"`
	ScoreSelfplay float64  `json:"scoreSelfplay"`
	ScoreStdev    float64  `json:"scoreStdev"`
	Utility       float64  `json:"utility"`
	UtilityLCB    float64  `json:"utilityLcb"`
	Visits        int      `json:"visits"`
	Weight        float64  `json:"weight"`
	Winrate       float64  `json:"winrate"`
}

type RootInfo struct {
	CurrentPlayer         string  `json:"currentPlayer"`
	RawLead               float64 `json:"rawLead"`
	RawNoResultProb       float64 `json:"rawNoResultProb"`
	RawScoreSelfplay      float64 `json:"rawScoreSelfplay"`
	RawScoreSelfplayStdev float64 `json:"rawScoreSelfplayStdev"`
	RawStScoreError       float64 `json:"rawStScoreError"`
	RawStWrError          float64 `json:"rawStWrError"`
	RawVarTimeLeft        float64 `json:"rawVarTimeLeft"`
	RawWinrate            float64 `json:"rawWinrate"`
	ScoreLead             float64 `json:"scoreLead"`
	ScoreSelfplay         float64 `json:"scoreSelfplay"`
	ScoreStdev            float64 `json:"scoreStdev"`
	SymHash               string  `json:"symHash"`
	ThisHash              string  `json:"thisHash"`
	Utility               float64 `json:"utility"`
	Visits                int     `json:"visits"`
	Weight                float64 `json:"weight"`
	Winrate               float64 `json:"winrate"`
}
