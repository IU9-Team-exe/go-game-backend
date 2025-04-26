package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"team_exe/internal/adapters"
	_ "team_exe/internal/domain/user"
	errs "team_exe/internal/errors"
	"team_exe/internal/httpresponse"
	"team_exe/internal/repository"
	authUC "team_exe/internal/usecase/auth"
	"team_exe/internal/utils"

	"go.uber.org/zap"
)

type AuthHandler struct {
	UsecaseHandler *authUC.UserUsecaseHandler
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
		UsecaseHandler: authUC.NewUserUsecaseHandler(
			repository.NewMongoUserStorage(mongo),
			repository.NewSessionRedisStorage(redis.GetClient()),
		),
		log: log,
	}
}

// Register godoc
// @Summary      Регистрация пользователя
// @Description  Создаёт нового пользователя и сразу устанавливает cookie `sessionID`.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        payload  body      RegisterRequest            true  "Учётные данные"
// @Success      200      {object}  httpresponse.Response      "Ответ без тела — только Status"
// @Failure      400      {object}  httpresponse.Response      "Некорректный запрос (например, плохой JSON)"
// @Failure      500      {object}  httpresponse.Response      "Внутренняя ошибка сервера"
// @Router       /register [post]
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

	sessionID, err := a.UsecaseHandler.RegisterUser(registerData.Username, registerData.Email, registerData.Password)
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
		Secure:   false,
		HttpOnly: true,
	})

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, nil)
}

// Login godoc
// @Summary      Вход пользователя
// @Description  Проверяет логин/пароль и устанавливает cookie `sessionID`.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        payload  body      LoginRequest               true  "Логин и пароль"
// @Success      200      {object}  httpresponse.Response
// @Failure      400      {object}  httpresponse.Response      "Пользователь не найден / неверный пароль / плохой JSON"
// @Failure      500      {object}  httpresponse.Response
// @Router       /login [post]
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

	sessionID, err := a.UsecaseHandler.LoginUser(loginData.Username, loginData.Password)
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
		Secure:   false, // TODO
		HttpOnly: true,
	})

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, nil)
}

// Logout godoc
// @Summary      Выход пользователя
// @Description  Удаляет сессию, указанную в cookie `sessionID`.
// @Tags         auth
// @Security     ApiKeyAuth
// @Produce      json
// @Success      200  {object}  httpresponse.Response
// @Failure      400  {object}  httpresponse.Response          "Нет cookie или сессия не найдена"
// @Router       /logout [post]
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

	if err := a.UsecaseHandler.LogoutUser(sessionCookie.Value); err != nil {
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

	userID, err := a.UsecaseHandler.GetUserIdFromSession(sessionCookie.Value)
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
// @Summary      Получить данные пользователя
// @Description  Возвращает пользователя по его ID. Требуется авторизация по cookie.
// @Tags         user
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        payload  body      UserFindRequest            true  "ID пользователя"
// @Success      200      {object}  user.User
// @Failure      400      {object}  httpresponse.Response      "Некорректный JSON или пользователь не найден"
// @Failure      401      {object}  httpresponse.Response      "Пользователь не авторизован"
// @Router       /getUserById [post]
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

	if !a.UsecaseHandler.CheckAuthorized(ctx, sessionCookie.Value) {
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

	user, err := a.UsecaseHandler.GetUserByUserId(ctx, req.UserID)
	if err != nil {
		a.log.Errorf("GetUserByID: error retrieving user by ID %s: %v", req.UserID, err)
		httpresponse.WriteResponseWithStatus(w, http.StatusBadRequest, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, user)
}
