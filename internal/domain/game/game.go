package game

import (
	"time"

	"github.com/gorilla/websocket"
)

// @name Game
type Game struct {
	GameKeySecret string          `json:"game_key" bson:"game_key"` // уникальный ключ
	GameKeyPublic string          `json:"game_key_public" bson:"game_key_public"`
	Users         []*GameUser     `json:"users" bson:"users"`
	CreatedAt     time.Time       `json:"created_at" bson:"created_at"`
	StartedAt     *time.Time      `json:"started_at,omitempty" bson:"started_at,omitempty"`
	Status        string          `json:"status" bson:"status"`
	BoardSize     int             `json:"board_size" bson:"board_size"`
	CurrentTurn   string          `json:"current_turn" bson:"current_turn"`
	Moves         []Move          `json:"moves" bson:"moves"`
	WhoIsNext     string          `json:"who_is_next" bson:"who_is_next"` // color
	PlayerBlack   string          `json:"player_black" bson:"player_black"`
	PlayerWhite   string          `json:"player_white" bson:"player_white"`
	PlayerBlackWS *websocket.Conn `json:"-"`
	PlayerWhiteWS *websocket.Conn `json:"-"`
	Komi          float64         `json:"komi" bson:"komi"`
	Sgf           string          `json:"sgf" bson:"sgf"`
}

// @name GameFromArchive
type GameFromArchive struct {
	BlackPlayer string    `bson:"black_player"`
	WhitePlayer string    `bson:"white_player"`
	Date        time.Time `bson:"date"`
	Moves       []Move    `bson:"moves"`
	Komi        float64   `bson:"komi"`
	Rules       string    `bson:"rules"`
	Result      Result    `bson:"result"`
	BlackRank   string    `bson:"black_rank"`
	WhiteRank   string    `bson:"white_rank"`
	Event       string    `bson:"event"`
	BoardSize   int       `bson:"board_size"`
	Sgf         string    `bson:"sgf"`
}

// @name Result
type Result struct {
	WinColor  string  `bson:"win_color"`
	PointDiff float64 `bson:"point_diff"`
}

// @name GameUser
type GameUser struct {
	ID     string          `json:"id" bson:"id"`
	Role   string          `json:"role" bson:"role"`
	Color  string          `json:"color" bson:"color"`
	Rating float64         `json:"rating" bson:"rating"`
	Score  float64         `json:"score" bson:"score"`
	WS     *websocket.Conn `json:"-"`
}

// @name GameCreateResponse
type GameCreateResponse struct {
	UniqueKey string `json:"public_key" bson:"public_key"`
}

// @name GameJoinRequest
type GameJoinRequest struct {
	GameKeyPublic string `json:"public_key" bson:"public_key"`
	Role          string `json:"role" bson:"role"`
}

// @name GameLeaveRequest
type GameLeaveRequest struct {
	GameKeyPublic string `json:"public_key" bson:"public_key"`
}

// @name GameStateResponse
type GameStateResponse struct {
	Move Move   `json:"move"`
	SGF  string `json:"sgf"`
}

// @name GetGameInfoRequest
type GetGameInfoRequest struct {
	GamePublicKey string `json:"game_key" bson:"game_key"`
}

// @name GetGameInfoResponse
type GetGameInfoResponse struct {
	Game                Game   `json:"game"`
	PlayerBlackNickname string `json:"player_black_nickname" bson:"player_black_nickname"`
	PlayerWhiteNickname string `json:"player_white_nickname" bson:"player_white_nickname"`
}

// @name CreateGameRequest
type CreateGameRequest struct {
	BoardSize      int     `json:"board_size" bson:"board_size"`
	Komi           float64 `json:"komi" bson:"komi"`
	IsCreatorBlack bool    `json:"is_creator_black" bson:"is_creator_black"`
}

// @name ArchiveResponse
type ArchiveResponse struct {
	Games             []GameFromArchive `json:"games" bson:"games"`
	TotalCountOfGames int               `json:"total" bson:"total"`
	Page              int               `json:"page" bson:"page"`
	PagesTotal        int               `json:"pages_total" bson:"pages_total"`
}

// @name ArchiveYearsResponse
type ArchiveYearsResponse struct {
	Years []YearGameStruct `json:"years" bson:"years"`
}

// @name YearGameStruct
type YearGameStruct struct {
	Year         int `json:"year" bson:"year"`
	CountOfGames int `json:"count_of_games" bson:"count_of_games"`
}

// @name ArchiveNamesResponse
type ArchiveNamesResponse struct {
	Names             []NameGameStruct `json:"names" bson:"names"`
	TotalCountOfNames int              `json:"total" bson:"total"`
	Page              int              `json:"page" bson:"page"`
	PagesTotal        int              `json:"pages_total" bson:"pages_total"`
}

// @name NameGameStruct
type NameGameStruct struct {
	Name         string `json:"name" bson:"name"`
	CountOfGames int    `json:"count_of_games" bson:"count_of_games"`
}

type GetMoveExplanationRequest struct {
	GameID        string `json:"game_archive_id"`
	MoveSeqNumber int    `json:"move_seq_number"`
}

type MoveExplanationResponse struct {
	LlmResponse string `json:"llm_response"`
}
