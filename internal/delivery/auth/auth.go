package auth

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"team_exe/internal/httpresponse"
	repo "team_exe/internal/repository"
	authUC "team_exe/internal/usecase/auth"
	"time"

	"github.com/redis/go-redis/v9"
)

type AuthHandler struct {
	usecaseHandler *authUC.AuthUsecaseHandler
}

func NewMapAuthHandler(redis *redis.Client) *AuthHandler {
	return &AuthHandler{usecaseHandler: authUC.NewUserUsecaseHandler(repo.NewMapUserStorage(),
		repo.NewSessionRedisStorage(redis))}
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
		httpresponse.WriteResponseWithStatus(w, 400, httpresponse.ErrorResponse{ErrorDescription: httpresponse.MALFORMEDJSON_errorDesc})
		return
	}
	sessionID, err := a.usecaseHandler.LoginUser(loginData.Username, loginData.Password)
	if err != nil {
		if errors.Is(err, authUC.ErrUserNotFound) {
			httpresponse.WriteResponseWithStatus(w, 400, httpresponse.ErrorResponse{ErrorDescription: "Пользователь не найден"})
			return
		} else if errors.Is(err, authUC.ErrWrongPassword) {
			httpresponse.WriteResponseWithStatus(w, 400, httpresponse.ErrorResponse{ErrorDescription: "Неверный пароль"})
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
	httpresponse.WriteResponseWithStatus(w, 200, nil)
}

func (a *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	sessionIDCookie, err := r.Cookie("sessionID")
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			httpresponse.WriteResponseWithStatus(w, 400, httpresponse.ErrorResponse{ErrorDescription: http.ErrNoCookie.Error()})
			return
		}
	}
	err = a.usecaseHandler.LogoutUser(sessionIDCookie.Value)
	if err != nil {
		httpresponse.WriteResponseWithStatus(w, 400, httpresponse.ErrorResponse{ErrorDescription: err.Error()})
		return
	}
	httpresponse.WriteResponseWithStatus(w, 200, nil)
}
