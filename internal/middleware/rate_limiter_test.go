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

// storeCase — одна реализация RateStore для прогона общих тестов
type storeCase struct {
	name string

	// build создает чистое хранилище и функцию, которая "перематывает" его время вперед на d:
	// у memory для этого сдвигается начало окна, у redis — TTL ключей через miniredis
	build func(t *testing.T) (store RateStore, advance func(d time.Duration))
}

func storeCases() []storeCase {
	return []storeCase{
		{
			name: "memory",
			build: func(t *testing.T) (RateStore, func(time.Duration)) {
				store := NewMemoryRateStore()

				return store, func(d time.Duration) {
					store.mu.Lock()
					defer store.mu.Unlock()

					store.windowStart = store.windowStart.Add(-d)
				}
			},
		},
		{
			name: "redis",
			build: func(t *testing.T) (RateStore, func(time.Duration)) {
				store, mr := newTestRedisStore(t)

				return store, func(d time.Duration) { mr.FastForward(d) }
			},
		},
	}
}

// forEachStore запускает один и тот же тест на каждой реализации RateStore
// Так RateLimiter проверяется на том, что он ведет себя одинаково с любым хранилищем
func forEachStore(t *testing.T, test func(t *testing.T, store RateStore, advance func(d time.Duration))) {
	t.Helper()

	for _, tc := range storeCases() {
		t.Run(tc.name, func(t *testing.T) {
			store, advance := tc.build(t)
			test(t, store, advance)
		})
	}
}

// requestWithUser создает запрос с userID в контексте — так, как это делает AuthMiddleware
func requestWithUser(userID int64) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/links", nil)

	return req.WithContext(context.WithValue(req.Context(), userIDContextKey, userID))
}

// countingHandler возвращает хендлер, который считает свои вызовы и отвечает 201
func countingHandler(calls *int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*calls++
		w.WriteHeader(http.StatusCreated)
	})
}

// allow вызывает Allow и проверяет, что хранилище не вернуло ошибку —
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
	forEachStore(t, func(t *testing.T, store RateStore, _ func(time.Duration)) {
		rl := NewRateLimiter(store, 3, time.Hour)

		for i := 1; i <= 3; i++ {
			assert.True(t, allow(t, rl, 1), "запрос %d укладывается в лимит", i)
		}
	})
}

func TestRateLimiter_Allow_ExceedsLimit(t *testing.T) {
	forEachStore(t, func(t *testing.T, store RateStore, _ func(time.Duration)) {
		rl := NewRateLimiter(store, 2, time.Hour)

		require.True(t, allow(t, rl, 1))
		require.True(t, allow(t, rl, 1))

		assert.False(t, allow(t, rl, 1))
		assert.False(t, allow(t, rl, 1))
	})
}

// TestRateLimiter_Allow_MemoryStore_DoesNotCountRejected проверяет особенность только
// in-memory реализации: отклоненные запросы не увеличивают счетчик
// В Redis-реализации INCR идет до проверки лимита, и счетчик растет и на отклоненных запросах,
// поэтому в общие тесты эта проверка не входит
func TestRateLimiter_Allow_MemoryStore_DoesNotCountRejected(t *testing.T) {
	store := NewMemoryRateStore()
	rl := NewRateLimiter(store, 2, time.Hour)

	require.True(t, allow(t, rl, 1))
	require.True(t, allow(t, rl, 1))
	require.False(t, allow(t, rl, 1))
	require.False(t, allow(t, rl, 1))

	assert.Equal(t, 2, store.countFor(userKey(1)))
}

func TestRateLimiter_Allow_CountsUsersSeparately(t *testing.T) {
	forEachStore(t, func(t *testing.T, store RateStore, _ func(time.Duration)) {
		rl := NewRateLimiter(store, 1, time.Hour)

		require.True(t, allow(t, rl, 1))
		require.False(t, allow(t, rl, 1))

		assert.True(t, allow(t, rl, 2))
	})
}

func TestRateLimiter_Allow_ResetsAfterWindow(t *testing.T) {
	forEachStore(t, func(t *testing.T, store RateStore, advance func(time.Duration)) {
		rl := NewRateLimiter(store, 2, time.Hour)

		require.True(t, allow(t, rl, 1))
		require.True(t, allow(t, rl, 1))
		require.False(t, allow(t, rl, 1))

		// время перематывается вместо реального ожидания: тест не зависит от таймингов
		advance(2 * time.Hour)

		assert.True(t, allow(t, rl, 1))
		assert.True(t, allow(t, rl, 1))
		assert.False(t, allow(t, rl, 1), "в новом окне лимит действует так же")
	})
}

// TestRateLimiter_Allow_PassesKeyLimitAndWindowToStore фиксирует контракт между лимитером
// и хранилищем: именно этот формат ключа и эти настройки видит Redis-реализация
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
	forEachStore(t, func(t *testing.T, store RateStore, _ func(time.Duration)) {
		rl := NewRateLimiter(store, 2, time.Hour)

		calls := 0
		handler := rl.Middleware(countingHandler(&calls))

		for i := 0; i < 2; i++ {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, requestWithUser(1))

			assert.Equal(t, http.StatusCreated, rec.Code)
		}

		assert.Equal(t, 2, calls)
	})
}

func TestRateLimiter_Middleware_RejectsOverLimit(t *testing.T) {
	forEachStore(t, func(t *testing.T, store RateStore, _ func(time.Duration)) {
		rl := NewRateLimiter(store, 1, time.Hour)

		calls := 0
		handler := rl.Middleware(countingHandler(&calls))

		first := httptest.NewRecorder()
		handler.ServeHTTP(first, requestWithUser(1))
		require.Equal(t, http.StatusCreated, first.Code)

		second := httptest.NewRecorder()
		handler.ServeHTTP(second, requestWithUser(1))

		assert.Equal(t, http.StatusTooManyRequests, second.Code)
		assert.Contains(t, second.Body.String(), "превышен лимит создания ссылок")
		assert.Equal(t, 1, calls, "следующий хендлер не должен вызываться при превышении лимита")
	})
}

func TestRateLimiter_Middleware_SkipsRequestsWithoutUser(t *testing.T) {
	forEachStore(t, func(t *testing.T, store RateStore, _ func(time.Duration)) {
		rl := NewRateLimiter(store, 1, time.Hour)

		calls := 0
		handler := rl.Middleware(countingHandler(&calls))

		for i := 0; i < 3; i++ {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/links", nil))

			assert.Equal(t, http.StatusCreated, rec.Code)
		}

		assert.Equal(t, 3, calls)
	})
}

// TestRateLimiter_Middleware_FailsClosedOnStoreError проверяет, что при сбое хранилища запрос
// отклоняется, а не пропускается
// Хранилище нарочно отвечает allowed=true вместе с ошибкой:
// если бы лимитер смотрел на bool раньше, чем на ошибку, тест бы это поймал
func TestRateLimiter_Middleware_FailsClosedOnStoreError(t *testing.T) {
	store := &fakeRateStore{allowed: true, err: errors.New("хранилище недоступно")}
	rl := NewRateLimiter(store, 10, time.Minute)

	calls := 0
	handler := rl.Middleware(countingHandler(&calls))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithUser(1))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, 0, calls, "при ошибке хранилища запрос не должен доходить до хендлера")
}

// TestRateLimiter_Allow_Concurrent гоняет Allow из многих горутин одновременно
// Лимит равен числу запросов каждой горутины, поэтому все ее запросы обязаны пройти,
// а следующий — быть отклонен: потерянное обновление счетчика (гонка) пропустило бы лишний запрос
// Пользователь на горутину, поэтому результат детерминирован и не зависит от порядка планировщика
func TestRateLimiter_Allow_Concurrent(t *testing.T) {
	forEachStore(t, func(t *testing.T, store RateStore, _ func(time.Duration)) {
		const (
			goroutines           = 50
			requestsPerGoroutine = 20
		)

		rl := NewRateLimiter(store, requestsPerGoroutine, time.Hour)

		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(userID int64) {
				defer wg.Done()
				for j := 0; j < requestsPerGoroutine; j++ {
					allowed, err := rl.Allow(context.Background(), userID)
					assert.NoError(t, err)
					assert.True(t, allowed, "запрос %d пользователя %d укладывается в лимит", j+1, userID)
				}
			}(int64(i))
		}
		wg.Wait()

		for i := 0; i < goroutines; i++ {
			assert.False(t, allow(t, rl, int64(i)), "запрос пользователя %d сверх лимита должен быть отклонен", i)
		}
	})
}
