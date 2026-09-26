package cache_test

import (
	"testing"

	"github.com/nikolaykonkin/shortlink/internal/cache"
	"github.com/nikolaykonkin/shortlink/internal/model"
)

// BenchmarkMemoryCache_Get_Hit измеряет попадание в кэш под параллельной нагрузкой — так выглядит
// реальный паттерн редиректов: много горутин одновременно читают один и тот же ключ,
// кэш защищен sync.RWMutex, и именно параллельные чтения RWMutex обязан пропускать без блокировок
func BenchmarkMemoryCache_Get_Hit(b *testing.B) {
	c := cache.NewMemoryCache()
	link := &model.Link{
		ID:          1,
		ShortCode:   "abc123",
		OriginalURL: "https://example.com",
	}
	c.Set(link.ShortCode, link)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = c.Get(link.ShortCode)
		}
	})
}

// BenchmarkMemoryCache_Get_Miss измеряет промах мимо кэша
// Последовательно: промах — не характерный для редиректа паттерн (обычно попадание),
// поэтому нет смысла имитировать параллельную нагрузку
func BenchmarkMemoryCache_Get_Miss(b *testing.B) {
	c := cache.NewMemoryCache()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = c.Get("nonexistent")
	}
}

// BenchmarkMemoryCache_Set измеряет запись в кэш
// Запись — редкая операция (только при создании ссылки), поэтому бенчмарк последовательный
func BenchmarkMemoryCache_Set(b *testing.B) {
	c := cache.NewMemoryCache()
	link := &model.Link{
		ID:          1,
		ShortCode:   "abc123",
		OriginalURL: "https://example.com",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		c.Set(link.ShortCode, link)
	}
}
