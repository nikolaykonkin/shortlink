package handler

import (
	"encoding/json"
	"net/http"

	"github.com/nikolaykonkin/shortlink/internal/middleware"
	"github.com/nikolaykonkin/shortlink/internal/model"
	"github.com/nikolaykonkin/shortlink/internal/service"
)

// LinkHandler обрабатывает создание ссылок и редирект по короткому коду
type LinkHandler struct {
	links *service.LinkService
}

// NewLinkHandler создает хендлер ссылок
func NewLinkHandler(links *service.LinkService) *LinkHandler {
	return &LinkHandler{links: links}
}

// Create обрабатывает POST /api/links
func (h *LinkHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "не удалось определить пользователя", http.StatusUnauthorized)
		return
	}

	var req model.LinkCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "некорректное тело запроса", http.StatusBadRequest)
		return
	}

	link, err := h.links.Create(r.Context(), userID, req)
	if err != nil {
		respondError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, link)
}

// Redirect обрабатывает GET /{shortCode}
func (h *LinkHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	shortCode := r.PathValue("shortCode")

	link, err := h.links.Resolve(r.Context(), shortCode)
	if err != nil {
		respondError(w, err)
		return
	}

	http.Redirect(w, r, link.OriginalURL, http.StatusFound)
}
