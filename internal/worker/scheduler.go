package worker

import (
	"context"
	"log"
	"time"

	"github.com/nikolaykonkin/shortlink/internal/repository"
)

// Scheduler периодически удаляет из хранилища ссылки с истекшим сроком жизни
type Scheduler struct {
	links    repository.LinkRepository
	interval time.Duration
	stop     chan struct{}
	done     chan struct{}
}

// NewScheduler создает планировщик очистки, interval — пауза между проходами
func NewScheduler(links repository.LinkRepository, interval time.Duration) *Scheduler {
	return &Scheduler{
		links:    links,
		interval: interval,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Run очищает просроченные ссылки по тикеру, пока не будет вызван Stop или отменен ctx
// Первый проход выполняется через interval после запуска, а не сразу: пропущенная
// очистка ничего не теряет, следующий тик удалит то же самое
func (s *Scheduler) Run(ctx context.Context) {
	defer close(s.done)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.purge(ctx)
		case <-s.stop:
			return
		case <-ctx.Done():
			return
		}
	}
}

// purge делает один проход очистки, ошибка не останавливает планировщик —
// следующий тик повторит попытку
func (s *Scheduler) purge(ctx context.Context) {
	deleted, err := s.links.DeleteExpired(ctx, time.Now())
	if err != nil {
		log.Printf("очистка просроченных ссылок: %v", err)
		return
	}

	// пустые проходы не логируем — иначе лог заполняется строками «удалено 0» на каждый тик
	if deleted > 0 {
		log.Printf("удалено просроченных ссылок: %d", deleted)
	}
}

// Stop останавливает планировщик и ждет выхода Run
// Если в этот момент идет проход очистки, Stop дождется его завершения
func (s *Scheduler) Stop() {
	close(s.stop)
	<-s.done
}
