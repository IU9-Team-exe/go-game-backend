package auth

import (
	"context"
	"fmt"

	"team_exe/internal/domain/user"
	"team_exe/internal/errors"
	"team_exe/internal/random"
)

type UserUsecaseHandler struct {
	userStorage    UserStorage
	sessionStorage SessionStorage
}

// UserStorage описывает операции с данными пользователя.
type UserStorage interface {
	CheckExists(username string) bool
	GetUser(username string) (user.User, bool)
	GetUserByID(ctx context.Context, userID string) (user.User, error)
	CreateUser(username, email, password string) (user.User, error)
	AddLose(ctx context.Context, userID string) error
}

// SessionStorage описывает операции над сессиями (чтение, запись, удаление).
type SessionStorage interface {
	GetUserIdBySession(sessionID string) (string, bool)
	StoreSession(sessionID, userID string)
	DeleteSession(sessionID string) bool
}

// NewUserUsecaseHandler создает новый обработчик бизнес-логики для аутентификации.
func NewUserUsecaseHandler(u UserStorage, s SessionStorage) *UserUsecaseHandler {
	return &UserUsecaseHandler{
		userStorage:    u,
		sessionStorage: s,
	}
}

// RegisterUser создает пользователя и автоматически логинит его (создаёт сессию).
// Может вернуть:
//   - errors.ErrUserExists, если пользователь с таким именем уже существует
//   - errors.ErrInternal, если произошла внутренняя ошибка
func (a *UserUsecaseHandler) RegisterUser(username, email, password string) (string, error) {
	// TODO делать дополнительную валидацию username/email/password
	createdUser, err := a.userStorage.CreateUser(username, email, password)
	if err != nil {
		return "", err
	}
	sessionID, err := a.LoginUser(createdUser.Username, password)
	if err != nil {
		return "", err
	}
	return sessionID, nil
}

// CheckAuthorized проверяет, есть ли валидная сессия по данному sessionID.
func (a *UserUsecaseHandler) CheckAuthorized(ctx context.Context, sessionID string) bool {
	_, found := a.sessionStorage.GetUserIdBySession(sessionID)
	return found
}

// GetUserBySessionId возвращает данные пользователя по sessionID.
func (a *UserUsecaseHandler) GetUserBySessionId(ctx context.Context, sessionID string) (user.User, error) {
	userID, found := a.sessionStorage.GetUserIdBySession(sessionID)
	if !found {
		return user.User{}, fmt.Errorf("user not found by session id: %s", sessionID)
	}
	return a.userStorage.GetUserByID(ctx, userID)
}

// GetUserByUserId возвращает пользователя по его userID.
func (a *UserUsecaseHandler) GetUserByUserId(ctx context.Context, userID string) (user.User, error) {
	return a.userStorage.GetUserByID(ctx, userID)
}

// LoginUser проверяет существование пользователя и правильность пароля.
// Возвращает sessionID, если все ок. Может вернуть:
//   - errors.ErrUserNotFound, если пользователь не найден
//   - errors.ErrWrongPassword, если неверный пароль
func (a *UserUsecaseHandler) LoginUser(providedUsername, providedPassword string) (string, error) {
	if !a.userStorage.CheckExists(providedUsername) {
		return "", errors.ErrUserNotFound
	}

	userFromDb, _ := a.userStorage.GetUser(providedUsername)
	if providedPassword != userFromDb.PasswordHash {
		return "", errors.ErrWrongPassword
	}

	sessionID := random.RandString(64)
	a.sessionStorage.StoreSession(sessionID, userFromDb.ID)
	return sessionID, nil
}

// LogoutUser удаляет сессию пользователя. Может вернуть errors.ErrSessionNotFound.
func (a *UserUsecaseHandler) LogoutUser(sessionID string) error {
	if _, ok := a.sessionStorage.GetUserIdBySession(sessionID); !ok {
		return errors.ErrSessionNotFound
	}
	if !a.sessionStorage.DeleteSession(sessionID) {
		return errors.ErrSessionNotFound
	}
	return nil
}

// GetUserIdFromSession возвращает userID по sessionID или ошибку ErrSessionNotFound.
func (a *UserUsecaseHandler) GetUserIdFromSession(sessionID string) (string, error) {
	userID, ok := a.sessionStorage.GetUserIdBySession(sessionID)
	if !ok {
		return "", errors.ErrSessionNotFound
	}
	return userID, nil
}

func (a *UserUsecaseHandler) AddLose(userID string) error {
	err := a.userStorage.AddLose(context.Background(), userID)
	if err != nil {
		return err
	}
	return nil
}
