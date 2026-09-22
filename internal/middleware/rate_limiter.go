package middleware

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter ограничивает число запросов пользователя в фиксированном окне времени
//
// Окно одно на всех пользователей и сбрасывается целиком: когда с его начала прошло
// window, все счетчики обнуляются разом (fixed window, а не скользящее окно)
type RateLimiter struct {
	mu          sync.Mutex
	counters    map[int64]int
	windowStart time.Time
	limit       int
	window      time.Duration
}

// NewRateLimiter создает лимитер: не более limit запросов от одного пользователя за window
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		counters:    make(map[int64]int),
		windowStart: time.Now(),
		limit:       limit,
		window:      window,
	}
}

// Allow учитывает запрос userID и сообщает, укладывается ли он в лимит текущего окна
// Отклоненный запрос счетчик не увеличивает
//
// Allow вызывается из горутины каждого запроса, поэтому counters и windowStart читаются и пишутся конкурентно,
// мьютекс держится на все тело функции — критическая секция и так короткая, а RWMutex не даст выигрыша:
// обновление счетчика внутри секции — это запись, к тому же сброс окна периодически перезаписывает всю карту
func (rl *RateLimiter) Allow(userID int64) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if time.Since(rl.windowStart) >= rl.window {
		rl.counters = make(map[int64]int)
		rl.windowStart = time.Now()
	}

	if rl.counters[userID] >= rl.limit {
		return false
	}

	rl.counters[userID]++

	return true
}

// countFor возвращает текущий счетчик пользователя в этом окне — только для тестов
func (rl *RateLimiter) countFor(userID int64) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	return rl.counters[userID]
}

// Middleware отвечает 429 на запросы пользователя сверх лимита
//
// Пользователя берет из контекста, поэтому в цепочке должен стоять после AuthMiddleware.
// Запрос без пользователя в контексте пропускается: отказ неаутентифицированным — забота
// AuthMiddleware, а не лимитера
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := UserIDFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}

		if !rl.Allow(userID) {
			http.Error(w, "превышен лимит создания ссылок", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
