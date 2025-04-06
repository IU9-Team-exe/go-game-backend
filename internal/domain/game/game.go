package game

import (
	"github.com/gorilla/websocket"
	"time"
)

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

type Result struct {
	WinColor  string  `bson:"win_color"`
	PointDiff float64 `bson:"point_diff"`
}

type GameUser struct {
	ID     string          `json:"id" bson:"id"`
	Role   string          `json:"role" bson:"role"`
	Color  string          `json:"color" bson:"color"`
	Rating float64         `json:"rating" bson:"rating"`
	Score  float64         `json:"score" bson:"score"`
	WS     *websocket.Conn `json:"-"`
}

type GameCreateResponse struct {
	UniqueKey string `json:"public_key" bson:"public_key"`
}

type GameJoinRequest struct {
	GameKeyPublic string `json:"public_key" bson:"public_key"`
	Role          string `json:"role" bson:"role"`
}

type GameLeaveRequest struct {
	GameKeyPublic string `json:"public_key" bson:"public_key"`
}

type GameStateResponse struct {
	Move Move   `json:"move"`
	SGF  string `json:"sgf"`
}

type GetGameInfoRequest struct {
	GamePublicKey string `json:"game_key" bson:"game_key"`
}

type GetGameInfoResponse struct {
	Game                Game   `json:"game"`
	PlayerBlackNickname string `json:"player_black_nickname" bson:"player_black_nickname"`
	PlayerWhiteNickname string `json:"player_white_nickname" bson:"player_white_nickname"`
}

type CreateGameRequest struct {
	BoardSize      int     `json:"board_size" bson:"board_size"`
	Komi           float64 `json:"komi" bson:"komi"`
	IsCreatorBlack bool    `json:"is_creator_black" bson:"is_creator_black"`
}

type ArchiveResponse struct {
	Games             []GameFromArchive `json:"games" bson:"games"`
	TotalCountOfGames int               `json:"total" bson:"total"`
	Page              int               `json:"page" bson:"page"`
	PagesTotal        int               `json:"pages_total" bson:"pages_total"`
}
