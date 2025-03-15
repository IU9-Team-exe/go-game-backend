package delivery

import (
	"TP-Game/internal/auth/repo"
	"TP-Game/internal/auth/repo/session_redis"
	"TP-Game/internal/auth/usecase"
	"TP-Game/internal/common"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

type AuthHandler struct {
	usecaseHandler *usecase.AuthUsecaseHandler
}

func NewMapAuthHandler(redis *redis.Client) *AuthHandler {
	return &AuthHandler{usecaseHandler: usecase.NewUserUsecaseHandler(repo.NewMapUserStorage(),
		session_redis.NewSessionRedisStorage(redis))}
}

type loginRequest struct {
	Username string `json:"Username"`
	Password string `json:"Password"`
}

func (a *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error(err.Error())
		return
	}
	loginData := loginRequest{}
	err = json.Unmarshal(requestBody, &loginData)
	if err != nil {
		slog.Error(err.Error())
		common.WriteResponseWithStatus(w, 400, common.ErrorResponse{ErrorDescription: common.MALFORMEDJSON_errorDesc})
		return
	}
	sessionID, err := a.usecaseHandler.LoginUser(loginData.Username, loginData.Password)
	if err != nil {
		if errors.Is(err, usecase.ErrUserNotFound) {
			common.WriteResponseWithStatus(w, 400, common.ErrorResponse{ErrorDescription: "Пользователь не найден"})
			return
		} else if errors.Is(err, usecase.ErrWrongPassword) {
			common.WriteResponseWithStatus(w, 400, common.ErrorResponse{ErrorDescription: "Неверный пароль"})
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "sessionID",
		Value:    sessionID,
		Expires:  time.Now().Add(time.Hour * 10),
		Secure:   true,
		HttpOnly: true,
	})
	common.WriteResponseWithStatus(w, 200, nil)
}

func (a *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	sessionIDCookie, err := r.Cookie("sessionID")
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			common.WriteResponseWithStatus(w, 400, common.ErrorResponse{ErrorDescription: http.ErrNoCookie.Error()})
			return
		}
	}
	err = a.usecaseHandler.LogoutUser(sessionIDCookie.Value)
	if err != nil {
		common.WriteResponseWithStatus(w, 400, common.ErrorResponse{ErrorDescription: err.Error()})
		return
	}
	common.WriteResponseWithStatus(w, 200, nil)
}
