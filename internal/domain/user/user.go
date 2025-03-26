package user

type User struct {
	ID           string
	Username     string `json:"Username"`
	PasswordHash string
	PasswordSalt string
}
