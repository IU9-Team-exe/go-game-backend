package user

import "time"

type User struct {
	ID             string            `json:"id" bson:"_id,omitempty"`
	Username       string            `json:"Username"`
	Email          string            `json:"email" bson:"email"`
	CreatedAt      time.Time         `json:"created_at" bson:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at" bson:"updated_at"`
	Rating         int               `json:"rating" bson:"rating"`
	CurrentGameKey string            `json:"current_game_key,omitempty" bson:"current_game_key,omitempty"`
	AvatarURL      string            `json:"avatar_url,omitempty" bson:"avatar_url,omitempty"`
	Status         string            `json:"status,omitempty" bson:"status,omitempty"`
	SocialLinks    map[string]string `json:"social_links,omitempty" bson:"social_links,omitempty"`
	Coins          int               `json:"coins" bson:"coins"`
	Statistic      UserStatistic     `json:"statistic" bson:"statistic"`
	PasswordHash   string            `bson:"password_hash"`
	PasswordSalt   string            `bson:"password_salt"`
}

type UserStatistic struct {
	Wins         int      `json:"wins" bson:"wins"`
	Losses       int      `json:"losses" bson:"losses"`
	Draws        int      `json:"draws" bson:"draws"`
	Achievements []string `json:"achievements,omitempty" bson:"achievements,omitempty"`
}
