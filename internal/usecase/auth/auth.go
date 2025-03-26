package auth

import (
	"errors"
	userDomain "team_exe/internal/domain/user"
	"team_exe/internal/random"
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
	GetUserByID(id int) (userDomain.User, bool)
}

type SessionStorage interface {
	GetUserIdBySession(sessionID string) (userID string, ok bool)
	StoreSession(sessionID string, userID string)
	DeleteSession(sessionID string) (ok bool)
}

var (
	ErrUserNotFound    = errors.New("user with provided username was not found")
	ErrWrongPassword   = errors.New("wrong password")
	ErrSessionNotFound = errors.New("session was not found")
)

func (a *AuthUsecaseHandler) CheckAuthorized(sessionID string) (ok bool, user userDomain.User) {
	userID, found := a.sessionStorage.GetUserIdBySession(sessionID)
	if !found {
		return false, userDomain.User{}
	}
	user, ok = a.userStorage.GetUserByID(userID)
	if !ok {
		return false, userDomain.User{}
	}
	return ok, user
}

func (a *AuthUsecaseHandler) LoginUser(providedUsername string, providedPassword string) (sessionID string, err error) {
	exists := a.userStorage.CheckExists(providedUsername)
	if !exists {
		return "", ErrUserNotFound
	}

	userFromDb, _ := a.userStorage.GetUser(providedUsername)
	if providedPassword != userFromDb.PasswordHash {
		return "", ErrWrongPassword
	}

	sessionID = random.RandString(64)
	a.sessionStorage.StoreSession(sessionID, userFromDb.ID)
	return sessionID, nil
}

func (a *AuthUsecaseHandler) LogoutUser(sessionID string) error {
	_, ok := a.sessionStorage.GetUserIdBySession(sessionID)
	if !ok {
		return ErrSessionNotFound
	}
	if !a.sessionStorage.DeleteSession(sessionID) {
		return ErrSessionNotFound
	}
	return nil
}

// Новый метод для получения userID из сессии
func (a *AuthUsecaseHandler) GetUserIdFromSession(sessionID string) (string, error) {
	userID, ok := a.sessionStorage.GetUserIdBySession(sessionID)
	if !ok {
		return "", ErrSessionNotFound
	}
	return userID, nil
}
