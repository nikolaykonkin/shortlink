package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requestWithUser создает запрос с userID в контексте — так, как это делает AuthMiddleware
func requestWithUser(userID int64) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/links", nil)

	return req.WithContext(context.WithValue(req.Context(), userIDContextKey, userID))
}

func TestRateLimiter_Allow_WithinLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Hour)

	for i := 1; i <= 3; i++ {
		assert.True(t, rl.Allow(1), "запрос %d укладывается в лимит", i)
	}
}

func TestRateLimiter_Allow_ExceedsLimit(t *testing.T) {
	rl := NewRateLimiter(2, time.Hour)

	require.True(t, rl.Allow(1))
	require.True(t, rl.Allow(1))

	assert.False(t, rl.Allow(1))
	assert.False(t, rl.Allow(1))
	assert.Equal(t, 2, rl.countFor(1), "отклоненные запросы не должны увеличивать счетчик")
}

func TestRateLimiter_Allow_CountsUsersSeparately(t *testing.T) {
	rl := NewRateLimiter(1, time.Hour)

	require.True(t, rl.Allow(1))
	require.False(t, rl.Allow(1))

	assert.True(t, rl.Allow(2))
}

func TestRateLimiter_Allow_ResetsAfterWindow(t *testing.T) {
	rl := NewRateLimiter(2, time.Hour)

	require.True(t, rl.Allow(1))
	require.True(t, rl.Allow(1))
	require.False(t, rl.Allow(1))

	// начало окна сдвигается в прошлое вместо реального ожидания — тест не зависит от таймингов
	rl.windowStart = time.Now().Add(-2 * time.Hour)

	assert.True(t, rl.Allow(1))
	assert.True(t, rl.Allow(1))
	assert.False(t, rl.Allow(1), "в новом окне лимит действует так же")
	assert.WithinDuration(t, time.Now(), rl.windowStart, time.Minute)
}

func TestRateLimiter_Middleware_PassesRequestsWithinLimit(t *testing.T) {
	rl := NewRateLimiter(2, time.Hour)

	calls := 0
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusCreated)
	}))

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, requestWithUser(1))

		assert.Equal(t, http.StatusCreated, rec.Code)
	}

	assert.Equal(t, 2, calls)
}

func TestRateLimiter_Middleware_RejectsOverLimit(t *testing.T) {
	rl := NewRateLimiter(1, time.Hour)

	calls := 0
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusCreated)
	}))

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, requestWithUser(1))
	require.Equal(t, http.StatusCreated, first.Code)

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, requestWithUser(1))

	assert.Equal(t, http.StatusTooManyRequests, second.Code)
	assert.Contains(t, second.Body.String(), "превышен лимит создания ссылок")
	assert.Equal(t, 1, calls, "следующий хендлер не должен вызываться при превышении лимита")
}

func TestRateLimiter_Middleware_SkipsRequestsWithoutUser(t *testing.T) {
	rl := NewRateLimiter(1, time.Hour)

	calls := 0
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusCreated)
	}))

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/links", nil))

		assert.Equal(t, http.StatusCreated, rec.Code)
	}

	assert.Equal(t, 3, calls)
}
