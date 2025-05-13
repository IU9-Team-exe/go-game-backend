package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"team_exe/internal/adapters"
	"team_exe/internal/domain/user"
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
	UserID   string `json:"user_id,omitempty"`
	Username string `json:"username,omitempty"`
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
		a.log.Error("Register: only POST allowed")
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed")
		return
	}

	body, err := utils.ReadRequestBody(r)
	if err != nil {
		a.log.Error("Register: read body failed:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_request", "Failed to read request body")
		return
	}

	var req RegisterRequest
	if err := json.Unmarshal(body, &req); err != nil {
		a.log.Error("Register: malformed JSON:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_json", httpresponse.MALFORMEDJSON_errorDesc)
		return
	}

	sessionID, err := a.UsecaseHandler.RegisterUser(req.Username, req.Email, req.Password)
	if err != nil {
		// map domain error → HTTP
		if errors.Is(err, errs.ErrUserExists) {
			a.log.Warnf("Register: user exists: %s", req.Username)
			httpresponse.WriteAPIError(w, http.StatusConflict, "user_already_exists", "Пользователь с таким именем уже существует")
			return
		}
		status, code := errs.TranslateErr(err)
		a.log.Error("Register: internal error:", err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "sessionID",
		Value:    sessionID,
		Expires:  time.Now().Add(10 * time.Hour),
		HttpOnly: true,
	})

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", nil)
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
		a.log.Error("Login: only POST allowed")
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed")
		return
	}

	body, err := utils.ReadRequestBody(r)
	if err != nil {
		a.log.Error("Login: read body failed:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_request", "Failed to read request body")
		return
	}

	var req LoginRequest
	if err := json.Unmarshal(body, &req); err != nil {
		a.log.Error("Login: malformed JSON:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_json", httpresponse.MALFORMEDJSON_errorDesc)
		return
	}

	sessionID, err := a.UsecaseHandler.LoginUser(req.Username, req.Password)
	if err != nil {
		status, code := errs.TranslateErr(err)
		a.log.Warnf("Login: %v", err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "sessionID",
		Value:    sessionID,
		Expires:  time.Now().Add(10 * time.Hour),
		HttpOnly: true,
	})

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", nil)
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
		a.log.Error("Logout: only POST allowed")
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed")
		return
	}

	cookie, err := r.Cookie("sessionID")
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			a.log.Warn("Logout: no sessionID cookie")
			httpresponse.WriteAPIError(w, http.StatusBadRequest, "missing_cookie", "sessionID cookie is required")
			return
		}
		a.log.Error("Logout: cookie error:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	if err := a.UsecaseHandler.LogoutUser(cookie.Value); err != nil {
		status, code := errs.TranslateErr(err)
		a.log.Error("Logout: failed:", err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", nil)
}

// GetUserID возвращает из сессии идентификатор пользователя.
// Если сессия просрочена или не найдена, пишет ошибку в http-ответ и возвращает "".
func (a *AuthHandler) GetUserID(w http.ResponseWriter, r *http.Request) string {
	cookie, err := r.Cookie("sessionID")
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			a.log.Warn("GetUserID: missing cookie")
			httpresponse.WriteAPIError(w, http.StatusBadRequest, "missing_cookie", "sessionID cookie is required")
			return ""
		}
		a.log.Error("GetUserID: cookie error:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_request", err.Error())
		return ""
	}

	userID, err := a.UsecaseHandler.GetUserIdFromSession(cookie.Value)
	if err != nil {
		status, code := errs.TranslateErr(err)
		a.log.Warn("GetUserID: session lookup failed:", err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
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
		a.log.Error("GetUserByID: only POST allowed")
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed")
		return
	}

	userID := a.GetUserID(w, r)
	if userID == "" {
		// GetUserID has already written the error response
		return
	}

	var req UserFindRequest
	if err := utils.DecodeJSONRequest(r, &req); err != nil {
		a.log.Error("GetUserByID: JSON decode failed:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}

	user, err := a.UsecaseHandler.GetUserByUserId(r.Context(), req.UserID)
	if err != nil {
		status, code := errs.TranslateErr(err)
		a.log.Errorf("GetUserByID: lookup failed for %s: %v", req.UserID, err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", user)
}

// GetUserByUsername godoc
// @Summary      Получить данные пользователя
// @Description  Возвращает пользователя по его username. Требуется авторизация по cookie.
// @Tags         user
// @Security     ApiKeyAuth
// @Accept       json
// @Produce      json
// @Param        payload  body      UserFindRequest            true  "username пользователя"
// @Success      200      {object}  user.User
// @Failure      400      {object}  httpresponse.Response      "Некорректный JSON или пользователь не найден"
// @Failure      401      {object}  httpresponse.Response      "Пользователь не авторизован"
// @Router       /getUserByUsername [post]
func (a *AuthHandler) GetUserByUsername(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.log.Error("GetUserByID: only POST allowed")
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed")
		return
	}

	userID := a.GetUserID(w, r)
	if userID == "" {
		// GetUserID has already written the error response
		return
	}

	var req UserFindRequest
	if err := utils.DecodeJSONRequest(r, &req); err != nil {
		a.log.Error("GetUserByID: JSON decode failed:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}

	user, err := a.UsecaseHandler.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		status, code := errs.TranslateErr(err)
		a.log.Errorf("GetUserByID: lookup failed for %s: %v", req.UserID, err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", user)
}

func (a *AuthHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		a.log.Error("GetUserByID: only POST allowed")
		httpresponse.WriteAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST allowed")
		return
	}

	userID := a.GetUserID(w, r)
	if userID == "" {
		return
	}

	var newUserData user.User
	if err := utils.DecodeJSONRequest(r, &newUserData); err != nil {
		a.log.Error("GetUserByID: JSON decode failed:", err)
		httpresponse.WriteAPIError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}

	err := a.UsecaseHandler.UpdateUser(r.Context(), newUserData, userID)
	if err != nil {
		status, code := errs.TranslateErr(err)
		a.log.Errorf("Update userdata failed for %s: %v", newUserData.Username, err)
		httpresponse.WriteAPIError(w, status, code, err.Error())
		return
	}

	httpresponse.WriteResponseWithStatus(w, http.StatusOK, "", newUserData)
}
