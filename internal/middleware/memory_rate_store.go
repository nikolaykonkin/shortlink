package middleware

import (
	"context"
	"sync"
	"time"
)

// проверка на этапе компиляции: MemoryRateStore действительно реализует RateStore
var _ RateStore = (*MemoryRateStore)(nil)

// MemoryRateStore считает запросы в памяти процесса в фиксированном окне времени
//
// Окно одно на все ключи и сбрасывается целиком: когда с его начала прошло window,
// все счетчики обнуляются разом (fixed window, а не скользящее окно)
// Плюс — карта не растет бесконечно: старые ключи исчезают при каждом сбросе, отдельная очистка не нужна
//
// Окно общее, поэтому один MemoryRateStore рассчитан на одно значение window: если
// вызывать Allow с разными окнами, они будут сбрасывать счетчики друг друга
// Счетчики живут только в этом процессе — при нескольких инстансах каждый считает свой лимит,
// для общего счетчика нужен RedisRateStore
type MemoryRateStore struct {
	mu          sync.Mutex
	counters    map[string]int
	windowStart time.Time
}

// NewMemoryRateStore создает пустое хранилище, окно начинается с момента создания
func NewMemoryRateStore() *MemoryRateStore {
	return &MemoryRateStore{
		counters:    make(map[string]int),
		windowStart: time.Now(),
	}
}

// Allow учитывает запрос по ключу key и сообщает, укладывается ли он в лимит текущего окна
// Отклоненный запрос счетчик не увеличивает. Ошибка всегда nil: она есть только ради интерфейса
//
// ctx не используется — in-memory операция не ходит в сеть и выполняется за микросекунды,
// отменять в ней нечего
//
// Allow вызывается из горутины каждого запроса, поэтому counters и windowStart читаются и пишутся конкурентно,
// мьютекс держится на все тело функции — критическая секция и так короткая, а RWMutex не даст выигрыша:
// обновление счетчика внутри секции — это запись, к тому же сброс окна периодически перезаписывает всю карту
func (s *MemoryRateStore) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if time.Since(s.windowStart) >= window {
		s.counters = make(map[string]int)
		s.windowStart = time.Now()
	}

	if s.counters[key] >= limit {
		return false, nil
	}

	s.counters[key]++

	return true, nil
}

// countFor возвращает текущий счетчик ключа в этом окне — только для тестов
func (s *MemoryRateStore) countFor(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.counters[key]
}
