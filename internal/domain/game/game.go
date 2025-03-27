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
	GameKey string `json:"public_key" bson:"public_key"`
	Role    string `json:"role" bson:"role"`
}

type GameStateResponse struct {
	Move Move   `json:"move"`
	SGF  string `json:"sgf"`
}

type GetGameInfoRequest struct {
	GameKey string `json:"game_key" bson:"game_key"`
}

type CreateGameRequest struct {
	BoardSize      int     `json:"board_size" bson:"board_size"`
	Komi           float64 `json:"komi" bson:"komi"`
	IsCreatorBlack bool    `json:"is_creator_black" bson:"is_creator_black"`
}
