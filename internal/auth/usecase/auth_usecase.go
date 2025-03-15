package usecase

import (
	userDomain "TP-Game/domain/user"
	"TP-Game/internal/common"
	"errors"
)

type AuthUsecaseHandler struct {
	userStorage    UserStorage
	sessionStorage SessionStorage
}

func NewUserUsecaseHandler(u UserStorage, s SessionStorage) *AuthUsecaseHandler {
	return &AuthUsecaseHandler{
		userStorage:    u,
		sessionStorage: s,
	}
}

type UserStorage interface {
	CheckExists(username string) bool
	GetUser(username string) (userDomain.User, bool)
}

type SessionStorage interface {
	GetUserIdBySession(sessionID string) (userID int, ok bool)
	StoreSession(sessionID string, userID int)
	DeleteSession(sessionID string) (ok bool)
}

var (
	ErrUserNotFound    = errors.New("user with provided username was not found")
	ErrWrongPassword   = errors.New("wrong password")
	ErrSessionNotFound = errors.New("session was not found")
)

func (a *AuthUsecaseHandler) LoginUser(providedUsername string, providedPassword string) (sessionID string, err error) {
	exists := a.userStorage.CheckExists(providedUsername)
	if !exists {
		return "", ErrUserNotFound
	}
	userFromDb, _ := a.userStorage.GetUser(providedUsername)
	if providedPassword != userFromDb.PasswordHash {
		return "", ErrWrongPassword
	}
	sessionID = common.RandString(64)
	a.sessionStorage.StoreSession(sessionID, userFromDb.ID)
	return sessionID, err
}

// returns nil or ErrSessionNotFound
func (a *AuthUsecaseHandler) LogoutUser(sessionID string) (err error) {
	_, ok := a.sessionStorage.GetUserIdBySession(sessionID)
	if !ok {
		return ErrSessionNotFound
	}
	ok = a.sessionStorage.DeleteSession(sessionID)
	if !ok {
		return ErrSessionNotFound
	}
	return nil
}
