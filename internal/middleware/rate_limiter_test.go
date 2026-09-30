package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
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

// allow вызывает Allow и проверяет, что in-memory хранилище не вернуло ошибку —
// так тестам не приходится разбирать пару значений в каждой строке
func allow(t *testing.T, rl *RateLimiter, userID int64) bool {
	t.Helper()

	allowed, err := rl.Allow(context.Background(), userID)
	require.NoError(t, err)

	return allowed
}

// fakeRateStore — заглушка RateStore: отдает заранее заданный ответ и запоминает аргументы вызова
// Нужна, чтобы проверять RateLimiter отдельно от логики счетчиков
type fakeRateStore struct {
	allowed bool
	err     error

	gotKey    string
	gotLimit  int
	gotWindow time.Duration
}

func (f *fakeRateStore) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, error) {
	f.gotKey, f.gotLimit, f.gotWindow = key, limit, window

	return f.allowed, f.err
}

func TestRateLimiter_Allow_WithinLimit(t *testing.T) {
	rl := NewRateLimiter(NewMemoryRateStore(), 3, time.Hour)

	for i := 1; i <= 3; i++ {
		assert.True(t, allow(t, rl, 1), "запрос %d укладывается в лимит", i)
	}
}

func TestRateLimiter_Allow_ExceedsLimit(t *testing.T) {
	store := NewMemoryRateStore()
	rl := NewRateLimiter(store, 2, time.Hour)

	require.True(t, allow(t, rl, 1))
	require.True(t, allow(t, rl, 1))

	assert.False(t, allow(t, rl, 1))
	assert.False(t, allow(t, rl, 1))
	assert.Equal(t, 2, store.countFor(userKey(1)), "отклоненные запросы не должны увеличивать счетчик")
}

func TestRateLimiter_Allow_CountsUsersSeparately(t *testing.T) {
	rl := NewRateLimiter(NewMemoryRateStore(), 1, time.Hour)

	require.True(t, allow(t, rl, 1))
	require.False(t, allow(t, rl, 1))

	assert.True(t, allow(t, rl, 2))
}

func TestRateLimiter_Allow_ResetsAfterWindow(t *testing.T) {
	store := NewMemoryRateStore()
	rl := NewRateLimiter(store, 2, time.Hour)

	require.True(t, allow(t, rl, 1))
	require.True(t, allow(t, rl, 1))
	require.False(t, allow(t, rl, 1))

	// начало окна сдвигается в прошлое вместо реального ожидания — тест не зависит от таймингов
	store.windowStart = time.Now().Add(-2 * time.Hour)

	assert.True(t, allow(t, rl, 1))
	assert.True(t, allow(t, rl, 1))
	assert.False(t, allow(t, rl, 1), "в новом окне лимит действует так же")
	assert.WithinDuration(t, time.Now(), store.windowStart, time.Minute)
}

// TestRateLimiter_Allow_PassesKeyLimitAndWindowToStore фиксирует контракт между лимитером
// и хранилищем: именно этот формат ключа и эти настройки увидит Redis-реализация
func TestRateLimiter_Allow_PassesKeyLimitAndWindowToStore(t *testing.T) {
	store := &fakeRateStore{allowed: true}
	rl := NewRateLimiter(store, 7, 30*time.Second)

	allowed, err := rl.Allow(context.Background(), 42)

	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, "user:42", store.gotKey)
	assert.Equal(t, 7, store.gotLimit)
	assert.Equal(t, 30*time.Second, store.gotWindow)
}

func TestRateLimiter_Middleware_PassesRequestsWithinLimit(t *testing.T) {
	rl := NewRateLimiter(NewMemoryRateStore(), 2, time.Hour)

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
	rl := NewRateLimiter(NewMemoryRateStore(), 1, time.Hour)

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
	rl := NewRateLimiter(NewMemoryRateStore(), 1, time.Hour)

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

// TestRateLimiter_Middleware_FailsClosedOnStoreError проверяет, что при сбое хранилища запрос
// отклоняется, а не пропускается
// Хранилище нарочно отвечает allowed=true вместе с ошибкой:
// если бы лимитер смотрел на bool раньше, чем на ошибку, тест бы это поймал
func TestRateLimiter_Middleware_FailsClosedOnStoreError(t *testing.T) {
	store := &fakeRateStore{allowed: true, err: errors.New("хранилище недоступно")}
	rl := NewRateLimiter(store, 10, time.Minute)

	calls := 0
	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusCreated)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithUser(1))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, 0, calls, "при ошибке хранилища запрос не должен доходить до хендлера")
}

// TestRateLimiter_Allow_Concurrent гоняет Allow из многих горутин одновременно: каждая
// горутина обслуживает одного пользователя, поэтому итоговый счетчик детерминирован
// и проверяется точно — гонка между пользователями была бы видна как расхождение счетчиков
func TestRateLimiter_Allow_Concurrent(t *testing.T) {
	store := NewMemoryRateStore()
	rl := NewRateLimiter(store, 1000, time.Hour) // лимит заведомо выше нагрузки

	const (
		goroutines           = 50
		requestsPerGoroutine = 20
	)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(userID int64) {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				_, err := rl.Allow(context.Background(), userID)
				assert.NoError(t, err)
			}
		}(int64(i))
	}
	wg.Wait()

	for i := 0; i < goroutines; i++ {
		assert.Equal(t, requestsPerGoroutine, store.countFor(userKey(int64(i))),
			"счетчик пользователя %d должен быть равен числу его запросов", i)
	}
}
