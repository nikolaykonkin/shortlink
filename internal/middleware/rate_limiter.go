package middleware

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"
)

// RateLimiter ограничивает число запросов пользователя за окно времени
//
// Сам ничего не считает: счетчики живут в RateStore, а RateLimiter отвечает за HTTP-часть —
// достать пользователя из контекста, превратить его в ключ и ответить 429 или 503
type RateLimiter struct {
	store  RateStore
	limit  int
	window time.Duration
}

// NewRateLimiter создает лимитер: не более limit запросов от одного пользователя за window,
// счетчики хранятся в store
func NewRateLimiter(store RateStore, limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		store:  store,
		limit:  limit,
		window: window,
	}
}

// userKey строит ключ счетчика пользователя
// Префикс "user:" делает ключ самоописывающим и не даст ему столкнуться с ключами других лимитеров,
// если позже появится, например, лимит по IP
func userKey(userID int64) string {
	return "user:" + strconv.FormatInt(userID, 10)
}

// Allow учитывает запрос userID и сообщает, укладывается ли он в лимит текущего окна
// Ошибка означает, что хранилище не смогло ответить, и значение bool в этом случае использовать нельзя
func (rl *RateLimiter) Allow(ctx context.Context, userID int64) (bool, error) {
	return rl.store.Allow(ctx, userKey(userID), rl.limit, rl.window)
}

// Middleware отвечает 429 на запросы пользователя сверх лимита
//
// Пользователя берет из контекста, поэтому в цепочке должен стоять после AuthMiddleware
// Запрос без пользователя в контексте пропускается: отказ неаутентифицированным — забота
// AuthMiddleware, а не лимитера
//
// Если хранилище вернуло ошибку, запрос отклоняется с 503 (fail-closed), а не пропускается:
// молча снять лимит при сбое хранилища значило бы дать любому клиенту обойти защиту как раз
// в тот момент, когда система и так нездорова
// Решение принято здесь, а не в реализациях RateStore, чтобы все они вели себя одинаково
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := UserIDFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		allowed, err := rl.Allow(r.Context(), userID)
		if err != nil {
			log.Printf("rate limiter: хранилище счетчиков недоступно, запрос пользователя %d отклонен: %v", userID, err)
			http.Error(w, "сервис временно недоступен", http.StatusServiceUnavailable)
			return
		}

		if !allowed {
			http.Error(w, "превышен лимит создания ссылок", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
