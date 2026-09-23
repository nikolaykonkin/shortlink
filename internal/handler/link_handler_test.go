package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nikolaykonkin/shortlink/internal/apperrors"
	"github.com/nikolaykonkin/shortlink/internal/cache"
	"github.com/nikolaykonkin/shortlink/internal/middleware"
	"github.com/nikolaykonkin/shortlink/internal/model"
	"github.com/nikolaykonkin/shortlink/internal/repository"
	"github.com/nikolaykonkin/shortlink/internal/service"
)

// stubLinkRepository — in-memory реализация repository.LinkRepository для тестов хендлера,
// в духе fakeLinkRepository из internal/service: поведение задается функциями-полями
type stubLinkRepository struct {
	createFunc         func(link *model.Link) error
	createCalls        int
	getByShortCodeFunc func(shortCode string) (*model.Link, error)
	getByIDFunc        func(id int64) (*model.Link, error)
	deleteFunc         func(id int64) error
	deleteCalls        int
}

func (s *stubLinkRepository) Create(_ context.Context, link *model.Link) error {
	s.createCalls++
	return s.createFunc(link)
}

func (s *stubLinkRepository) GetByShortCode(_ context.Context, shortCode string) (*model.Link, error) {
	return s.getByShortCodeFunc(shortCode)
}

func (s *stubLinkRepository) GetByID(_ context.Context, id int64) (*model.Link, error) {
	return s.getByIDFunc(id)
}

func (s *stubLinkRepository) Delete(_ context.Context, id int64) error {
	s.deleteCalls++
	return s.deleteFunc(id)
}

func (s *stubLinkRepository) DeleteExpired(_ context.Context, _ time.Time) (int64, error) {
	return 0, errors.New("не используется в этих тестах")
}

var _ repository.LinkRepository = (*stubLinkRepository)(nil)

// stubClickRecorder — заглушка ClickRecorder, запоминающая linkID для проверки в Redirect-тестах
type stubClickRecorder struct {
	recorded []int64
}

func (s *stubClickRecorder) Record(linkID int64) {
	s.recorded = append(s.recorded, linkID)
}

var _ service.ClickRecorder = (*stubClickRecorder)(nil)

// stubClickStats — in-memory реализация ClickStats, как fakeClickStats из internal/service
type stubClickStats struct {
	counts map[int64]int64
}

func (s *stubClickStats) CountByLinkID(_ context.Context, linkID int64) (int64, error) {
	return s.counts[linkID], nil
}

var _ service.ClickStats = (*stubClickStats)(nil)

// newLinkTestMux собирает те же маршруты ссылок, что main.go, поверх настоящих
// LinkHandler, LinkService и AuthMiddleware — тест проверяет связку целиком,
// а не хендлер в отрыве от аутентификации и маршрутизации
func newLinkTestMux(repo *stubLinkRepository, clicks *stubClickRecorder, stats *stubClickStats) *http.ServeMux {
	svc := service.NewLinkService(repo, clicks, stats, cache.NewMemoryCache())
	h := NewLinkHandler(svc)
	requireAuth := middleware.AuthMiddleware([]byte(testJWTSecret))

	mux := http.NewServeMux()
	mux.Handle("POST /api/links", requireAuth(http.HandlerFunc(h.Create)))
	mux.Handle("DELETE /api/links/{id}", requireAuth(http.HandlerFunc(h.Delete)))
	mux.Handle("GET /api/links/{id}/stats", requireAuth(http.HandlerFunc(h.Stats)))
	mux.HandleFunc("GET /{shortCode}", h.Redirect)

	return mux
}

func TestLinkHandler_Create_Success(t *testing.T) {
	repo := &stubLinkRepository{
		createFunc: func(link *model.Link) error {
			link.ID = 1
			return nil
		},
	}
	mux := newLinkTestMux(repo, &stubClickRecorder{}, &stubClickStats{})

	req := httptest.NewRequest(http.MethodPost, "/api/links", strings.NewReader(`{"original_url":"https://example.com"}`))
	req.Header.Set("Authorization", "Bearer "+newTestToken(t, 42))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, 1, repo.createCalls)
}

func TestLinkHandler_Create_Unauthorized(t *testing.T) {
	repo := &stubLinkRepository{
		createFunc: func(_ *model.Link) error {
			t.Fatal("Create не должен вызываться без аутентификации")
			return nil
		},
	}
	mux := newLinkTestMux(repo, &stubClickRecorder{}, &stubClickStats{})

	req := httptest.NewRequest(http.MethodPost, "/api/links", strings.NewReader(`{"original_url":"https://example.com"}`))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Zero(t, repo.createCalls)
}

func TestLinkHandler_Delete_Success(t *testing.T) {
	repo := &stubLinkRepository{
		getByIDFunc: func(_ int64) (*model.Link, error) {
			return &model.Link{ID: 7, ShortCode: "abc1234", UserID: 42}, nil
		},
		deleteFunc: func(_ int64) error {
			return nil
		},
	}
	mux := newLinkTestMux(repo, &stubClickRecorder{}, &stubClickStats{})

	req := httptest.NewRequest(http.MethodDelete, "/api/links/7", nil)
	req.Header.Set("Authorization", "Bearer "+newTestToken(t, 42))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, 1, repo.deleteCalls)
}

func TestLinkHandler_Delete_Unauthorized(t *testing.T) {
	repo := &stubLinkRepository{
		getByIDFunc: func(_ int64) (*model.Link, error) {
			t.Fatal("GetByID не должен вызываться без аутентификации")
			return nil, nil
		},
	}
	mux := newLinkTestMux(repo, &stubClickRecorder{}, &stubClickStats{})

	req := httptest.NewRequest(http.MethodDelete, "/api/links/7", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLinkHandler_Delete_ForeignLinkIsForbidden(t *testing.T) {
	repo := &stubLinkRepository{
		getByIDFunc: func(_ int64) (*model.Link, error) {
			return &model.Link{ID: 7, ShortCode: "abc1234", UserID: 99}, nil
		},
	}
	mux := newLinkTestMux(repo, &stubClickRecorder{}, &stubClickStats{})

	req := httptest.NewRequest(http.MethodDelete, "/api/links/7", nil)
	req.Header.Set("Authorization", "Bearer "+newTestToken(t, 42))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Zero(t, repo.deleteCalls)
}

func TestLinkHandler_Delete_NotFound(t *testing.T) {
	repo := &stubLinkRepository{
		getByIDFunc: func(_ int64) (*model.Link, error) {
			return nil, apperrors.ErrLinkNotFound
		},
	}
	mux := newLinkTestMux(repo, &stubClickRecorder{}, &stubClickStats{})

	req := httptest.NewRequest(http.MethodDelete, "/api/links/7", nil)
	req.Header.Set("Authorization", "Bearer "+newTestToken(t, 42))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestLinkHandler_Delete_InvalidID(t *testing.T) {
	repo := &stubLinkRepository{
		getByIDFunc: func(_ int64) (*model.Link, error) {
			t.Fatal("GetByID не должен вызываться при некорректном id")
			return nil, nil
		},
	}
	mux := newLinkTestMux(repo, &stubClickRecorder{}, &stubClickStats{})

	req := httptest.NewRequest(http.MethodDelete, "/api/links/abc", nil)
	req.Header.Set("Authorization", "Bearer "+newTestToken(t, 42))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestLinkHandler_Redirect_Success(t *testing.T) {
	repo := &stubLinkRepository{
		getByShortCodeFunc: func(_ string) (*model.Link, error) {
			return &model.Link{ID: 7, ShortCode: "abc1234", OriginalURL: "https://example.com"}, nil
		},
	}
	clicks := &stubClickRecorder{}
	mux := newLinkTestMux(repo, clicks, &stubClickStats{})

	req := httptest.NewRequest(http.MethodGet, "/abc1234", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "https://example.com", rec.Header().Get("Location"))
	assert.Equal(t, []int64{7}, clicks.recorded)
}

func TestLinkHandler_Redirect_NotFound(t *testing.T) {
	repo := &stubLinkRepository{
		getByShortCodeFunc: func(_ string) (*model.Link, error) {
			return nil, apperrors.ErrLinkNotFound
		},
	}
	mux := newLinkTestMux(repo, &stubClickRecorder{}, &stubClickStats{})

	req := httptest.NewRequest(http.MethodGet, "/doesnotexist", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestLinkHandler_Stats_Success(t *testing.T) {
	repo := &stubLinkRepository{
		getByIDFunc: func(_ int64) (*model.Link, error) {
			return &model.Link{ID: 7, ShortCode: "abc1234", UserID: 42}, nil
		},
	}
	stats := &stubClickStats{counts: map[int64]int64{7: 15}}
	mux := newLinkTestMux(repo, &stubClickRecorder{}, stats)

	req := httptest.NewRequest(http.MethodGet, "/api/links/7/stats", nil)
	req.Header.Set("Authorization", "Bearer "+newTestToken(t, 42))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	resp := decodeJSON[model.LinkStatsResponse](t, rec)
	assert.Equal(t, int64(15), resp.ClickCount)
}

func TestLinkHandler_Stats_ForeignLinkIsForbidden(t *testing.T) {
	repo := &stubLinkRepository{
		getByIDFunc: func(_ int64) (*model.Link, error) {
			return &model.Link{ID: 7, ShortCode: "abc1234", UserID: 99}, nil
		},
	}
	mux := newLinkTestMux(repo, &stubClickRecorder{}, &stubClickStats{counts: map[int64]int64{7: 15}})

	req := httptest.NewRequest(http.MethodGet, "/api/links/7/stats", nil)
	req.Header.Set("Authorization", "Bearer "+newTestToken(t, 42))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestLinkHandler_Stats_NotFound(t *testing.T) {
	repo := &stubLinkRepository{
		getByIDFunc: func(_ int64) (*model.Link, error) {
			return nil, apperrors.ErrLinkNotFound
		},
	}
	mux := newLinkTestMux(repo, &stubClickRecorder{}, &stubClickStats{})

	req := httptest.NewRequest(http.MethodGet, "/api/links/7/stats", nil)
	req.Header.Set("Authorization", "Bearer "+newTestToken(t, 42))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
