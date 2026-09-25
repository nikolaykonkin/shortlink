package main

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
)

// fakePinger — in-memory реализация Pinger для тестов
//
// pingFunc делегирует вызов, как createFunc/getByIDFunc у fake-репозиториев в internal/service
// и internal/handler — поведение задается самим тестом, а не жестко зашито в структуру
type fakePinger struct {
	pingFunc func(ctx context.Context) error
}

func (f *fakePinger) Ping(ctx context.Context) error {
	return f.pingFunc(ctx)
}

func TestHealthHandler_DatabaseUp(t *testing.T) {
	db := &fakePinger{
		pingFunc: func(_ context.Context) error {
			return nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	healthHandler(db)(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestHealthHandler_DatabaseDown(t *testing.T) {
	db := &fakePinger{
		pingFunc: func(_ context.Context) error {
			return errors.New("сбой соединения с базой")
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	healthHandler(db)(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	// http.Error дописывает перевод строки в конец тела — сравниваем без него
	assert.Equal(t, "database unavailable", strings.TrimSpace(rec.Body.String()))
}

func TestHealthHandler_DatabaseTimeout(t *testing.T) {
	db := &fakePinger{
		pingFunc: func(ctx context.Context) error {
			select {
			case <-time.After(healthCheckTimeout + time.Second):
				return nil
			case <-ctx.Done():
				// именно так должен вести себя настоящий pgxpool.Pool.Ping —
				// он сам слушает переданный контекст и возвращает его ошибку
				return ctx.Err()
			}
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()

	healthHandler(db)(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, "database unavailable", strings.TrimSpace(rec.Body.String()))
}
