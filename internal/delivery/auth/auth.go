package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"go.uber.org/zap"
	"team_exe/internal/adapters"
	errs "team_exe/internal/errors"
	"team_exe/internal/httpresponse"
	"team_exe/internal/repository"
	authUC "team_exe/internal/usecase/auth"
	"team_exe/internal/utils"
)

type AuthHandler struct {
	usecaseHandler *authUC.AuthUsecaseHandler
	log            *zap.SugaredLogger
}

type RegisterRequest struct {
	Username string `json:"Username"`
	Email    string `json:"Email"`
	Password string `json:"Password"`
}

type LoginRequest struct {
	Username string `json:"Username"`
	Password string `json:"Password"`
}

type UserFindRequest struct {
	UserID string `json:"user_id"`
}

func NewAuthHandler(redis *adapters.AdapterRedis, mongo *adapters.AdapterMongo, log *zap.SugaredLogger) *AuthHandler {
	return &AuthHandler{
		usecaseHandler: authUC.NewUserUsecaseHandler(
			repository.NewMongoUserStorage(mongo),
			repository.NewSessionRedisStorage(redis.GetClient()),
		),
		log: log,
	}
}

// Register godoc
// @Summary Регистрация нового пользователя
// @Description Создаёт нового пользователя и устанавливает cookie sessionID
// @Tags auth
// @Accept json
// @Produce json
// @Param register body RegisterRequest true "Данные пользователя для регистрации"
// @Success 200 {string} string "OK"
// @Failure 400 {object} httpresponse.ErrorResponse
// @Failure 500 {object} httpresponse.ErrorResponse
// @Router /register [post]
func (a *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.log.Error("Register: only POST method is allowed")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	requestBody, err := utils.ReadRequestBody(r)
	if err != nil {
		a.log.Error("Register: failed to read request body: ", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest,
			httpresponse.ErrorResponse{ErrorDescription: "Failed to read request body"})
		return
	}

	var registerData RegisterRequest
	if err := json.Unmarshal(requestBody, &registerData); err != nil {
		a.log.Error("Register: malformed JSON: ", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest,
			httpresponse.ErrorResponse{ErrorDescription: httpresponse.MALFORMEDJSON_errorDesc})
		return
	}

	sessionID, err := a.usecaseHandler.RegisterUser(registerData.Username, registerData.Email, registerData.Password)
	if err != nil {
		if errors.Is(err, errs.ErrUserExists) {
			a.log.Errorf("Register: user already exists: %s", registerData.Username)
			httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest,
				httpresponse.ErrorResponse{ErrorDescription: "Пользователь с таким именем уже существует"})
			return
		}
		a.log.Error("Register: internal error: ", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusInternalServerError,
			httpresponse.ErrorResponse{ErrorDescription: err.Error()})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "sessionID",
		Value:    sessionID,
		Expires:  time.Now().Add(10 * time.Hour),
		Secure:   true,
		HttpOnly: true,
	})

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, nil)
}

// Login godoc
// @Summary Вход пользователя
// @Description Авторизует пользователя по логину и паролю, устанавливает cookie sessionID
// @Tags auth
// @Accept json
// @Produce json
// @Param login body LoginRequest true "Данные пользователя для входа"
// @Success 200 {string} string "OK"
// @Failure 400 {object} httpresponse.ErrorResponse
// @Failure 500 {object} httpresponse.ErrorResponse
// @Router /login [post]
func (a *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.log.Error("Login: only POST method is allowed")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	requestBody, err := utils.ReadRequestBody(r)
	if err != nil {
		a.log.Error("Login: failed to read request body: ", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest,
			httpresponse.ErrorResponse{ErrorDescription: "Failed to read request body"})
		return
	}

	var loginData LoginRequest
	if err := json.Unmarshal(requestBody, &loginData); err != nil {
		a.log.Error("Login: malformed JSON: ", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest,
			httpresponse.ErrorResponse{ErrorDescription: httpresponse.MALFORMEDJSON_errorDesc})
		return
	}

	sessionID, err := a.usecaseHandler.LoginUser(loginData.Username, loginData.Password)
	if err != nil {
		switch {
		case errors.Is(err, errs.ErrUserNotFound):
			a.log.Errorf("Login: user not found: %s", loginData.Username)
			httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest,
				httpresponse.ErrorResponse{ErrorDescription: "Пользователь не найден"})
			return
		case errors.Is(err, errs.ErrWrongPassword):
			a.log.Errorf("Login: wrong password for user: %s", loginData.Username)
			httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest,
				httpresponse.ErrorResponse{ErrorDescription: "Неверный пароль"})
			return
		default:
			a.log.Error("Login: internal error: ", err)
			httpresponse.WriteResponseWithStatus(w, http.StatusInternalServerError,
				httpresponse.ErrorResponse{ErrorDescription: err.Error()})
			return
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "sessionID",
		Value:    sessionID,
		Expires:  time.Now().Add(10 * time.Hour),
		Secure:   true,
		HttpOnly: true,
	})

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, nil)
}

// Logout godoc
// @Summary Выход пользователя
// @Description Удаляет сессию пользователя по cookie sessionID
// @Tags auth
// @Produce json
// @Success 200 {string} string "OK"
// @Failure 400 {object} httpresponse.ErrorResponse
// @Router /logout [post]
func (a *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.log.Error("Logout: only POST method is allowed")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	sessionCookie, err := r.Cookie("sessionID")
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			a.log.Warn("Logout: no cookie provided")
			httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest,
				httpresponse.ErrorResponse{ErrorDescription: http.ErrNoCookie.Error()})
			return
		}
		a.log.Error("Logout: error retrieving cookie: ", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest,
			httpresponse.ErrorResponse{ErrorDescription: err.Error()})
		return
	}

	if err := a.usecaseHandler.LogoutUser(sessionCookie.Value); err != nil {
		a.log.Errorf("Logout: failed to logout sessionID=%s: %v", sessionCookie.Value, err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest,
			httpresponse.ErrorResponse{ErrorDescription: err.Error()})
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, nil)
}

// GetUserID возвращает из сессии идентификатор пользователя.
// Если сессия просрочена или не найдена, пишет ошибку в http-ответ и возвращает "".
func (a *AuthHandler) GetUserID(w http.ResponseWriter, r *http.Request) string {
	sessionCookie, err := r.Cookie("sessionID")
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			a.log.Warn("GetUserID: no sessionID cookie")
			httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest,
				httpresponse.ErrorResponse{ErrorDescription: "Не найдена cookie sessionID"})
			return ""
		}
		a.log.Error("GetUserID: error retrieving cookie: ", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest,
			httpresponse.ErrorResponse{ErrorDescription: err.Error()})
		return ""
	}

	userID, err := a.usecaseHandler.GetUserIdFromSession(sessionCookie.Value)
	if err != nil {
		if errors.Is(err, errs.ErrSessionNotFound) {
			a.log.Warn("GetUserID: session not found or expired")
			httpresponse.WriteResponseWithStatus(w, http.StatusUnauthorized,
				httpresponse.ErrorResponse{ErrorDescription: "Сессия не найдена или истекла"})
			return ""
		}
		a.log.Error("GetUserID: internal error: ", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusInternalServerError,
			httpresponse.ErrorResponse{ErrorDescription: err.Error()})
		return ""
	}

	return userID
}

// GetUserByID godoc
// @Summary Получение информации о пользователе
// @Description Возвращает пользователя по ID. Требуется авторизация (cookie sessionID).
// @Tags user
// @Accept json
// @Produce json
// @Param user body UserFindRequest true "ID пользователя для поиска"
// @Success 200 {object} user.User
// @Failure 400 {object} httpresponse.ErrorResponse
// @Failure 401 {object} httpresponse.ErrorResponse
// @Failure 405 {string} string "Only POST method is allowed"
// @Router /getUserById [post]
func (a *AuthHandler) GetUserByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.log.Error("GetUserByID: only POST method is allowed")
		httpresponse.WriteResponseWithStatus(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	sessionCookie, err := r.Cookie("sessionID")
	if err != nil {
		a.log.Error("GetUserByID: cookie error: ", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()

	if !a.usecaseHandler.CheckAuthorized(ctx, sessionCookie.Value) {
		a.log.Warn("GetUserByID: unauthorized access attempt")
		httpresponse.WriteResponseWithStatus(w, http.StatusUnauthorized, "User is not authorized")
		return
	}

	var req UserFindRequest
	if err := utils.DecodeJSONRequest(r, &req); err != nil {
		a.log.Error("GetUserByID: JSON decode error: ", err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	user, err := a.usecaseHandler.GetUserByUserId(ctx, req.UserID)
	if err != nil {
		a.log.Errorf("GetUserByID: error retrieving user by ID %s: %v", req.UserID, err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, user)
}
