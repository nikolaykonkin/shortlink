package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/nikolaykonkin/shortlink/internal/model"
	"github.com/nikolaykonkin/shortlink/internal/service"
)

// AuthHandler обрабатывает регистрацию и вход
type AuthHandler struct {
	users *service.UserService
}

// NewAuthHandler создает хендлер аутентификации
func NewAuthHandler(users *service.UserService) *AuthHandler {
	return &AuthHandler{users: users}
}

// loginResponse — тело ответа на успешный вход
type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Register обрабатывает POST /api/register
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req model.UserCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "некорректное тело запроса", http.StatusBadRequest)
		return
	}

	user, err := h.users.Register(r.Context(), req)
	if err != nil {
		respondError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, user)
}

// Login обрабатывает POST /api/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req model.UserLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "некорректное тело запроса", http.StatusBadRequest)
		return
	}

	token, expiresAt, err := h.users.Login(r.Context(), req)
	if err != nil {
		respondError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{Token: token, ExpiresAt: expiresAt})
}
