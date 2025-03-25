package user

type User struct {
	ID           int
	Username     string `json:"Username"`
	PasswordHash string
	PasswordSalt string
}
