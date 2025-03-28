package auth

import (
	userDomain "team_exe/internal/domain/user"
	errors2 "team_exe/internal/errors"
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
	GetUserByID(id string) (userDomain.User, bool)
	CreateUser(username, email, password string) (userDomain.User, error)
}

type SessionStorage interface {
	GetUserIdBySession(sessionID string) (userID string, ok bool)
	StoreSession(sessionID string, userID string)
	DeleteSession(sessionID string) (ok bool)
}

// может вернуть ErrUserExists, ErrInternal
func (a *AuthUsecaseHandler) RegisterUser(username string, email string, password string) (sessionID string, err error) {
	//TODO проверить имя, почту и пароль на валидность
	user, err := a.userStorage.CreateUser(username, email, password)
	if err != nil {
		return "", err
	}
	sessionID, err = a.LoginUser(user.Username, password)
	if err != nil {
		return "", err
	}
	return sessionID, nil
}

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
		return "", errors2.ErrUserNotFound
	}

	userFromDb, _ := a.userStorage.GetUser(providedUsername)
	if providedPassword != userFromDb.PasswordHash {
		return "", errors2.ErrWrongPassword
	}

	sessionID = random.RandString(64)
	a.sessionStorage.StoreSession(sessionID, userFromDb.ID)
	return sessionID, nil
}

func (a *AuthUsecaseHandler) LogoutUser(sessionID string) error {
	_, ok := a.sessionStorage.GetUserIdBySession(sessionID)
	if !ok {
		return errors2.ErrSessionNotFound
	}
	if !a.sessionStorage.DeleteSession(sessionID) {
		return errors2.ErrSessionNotFound
	}
	return nil
}

// Новый метод для получения userID из сессии
func (a *AuthUsecaseHandler) GetUserIdFromSession(sessionID string) (string, error) {
	userID, ok := a.sessionStorage.GetUserIdBySession(sessionID)
	if !ok {
		return "", errors2.ErrSessionNotFound
	}
	return userID, nil
}
