package worker

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nikolaykonkin/shortlink/internal/model"
	"github.com/nikolaykonkin/shortlink/internal/repository"
)

// fakeLinkRepository — in-memory реализация repository.LinkRepository, потокобезопасная:
// DeleteExpired вызывается из горутины планировщика, а результаты читает горутина теста
// Остальные методы планировщику не нужны
type fakeLinkRepository struct {
	mu        sync.Mutex
	calls     []time.Time // аргумент before каждого вызова DeleteExpired, по порядку
	deleteErr error       // задается до запуска планировщика и в ходе теста не меняется
}

func (f *fakeLinkRepository) Create(_ context.Context, _ *model.Link) error {
	return errors.New("не используется в этих тестах")
}

func (f *fakeLinkRepository) GetByShortCode(_ context.Context, _ string) (*model.Link, error) {
	return nil, errors.New("не используется в этих тестах")
}

func (f *fakeLinkRepository) GetByID(_ context.Context, _ int64) (*model.Link, error) {
	return nil, errors.New("не используется в этих тестах")
}

func (f *fakeLinkRepository) Delete(_ context.Context, _ int64) error {
	return errors.New("не используется в этих тестах")
}

func (f *fakeLinkRepository) DeleteExpired(_ context.Context, before time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, before)

	return 0, f.deleteErr
}

func (f *fakeLinkRepository) callTimes() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.calls)
}

func (f *fakeLinkRepository) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.calls)
}

var _ repository.LinkRepository = (*fakeLinkRepository)(nil)

func TestScheduler_CallsDeleteExpiredPeriodically(t *testing.T) {
	repo := &fakeLinkRepository{}
	s := NewScheduler(repo, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	// один вызов мог быть случайностью, три подряд — это периодичность
	require.Eventually(t, func() bool {
		return repo.callCount() >= 3
	}, time.Second, 5*time.Millisecond)
}

func TestScheduler_PassesCurrentTime(t *testing.T) {
	repo := &fakeLinkRepository{}
	s := NewScheduler(repo, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	go s.Run(ctx)

	require.Eventually(t, func() bool {
		return repo.callCount() >= 1
	}, time.Second, 5*time.Millisecond)

	// граница должна быть на момент вызова, а не нулевым или зафиксированным временем
	assert.WithinRange(t, repo.callTimes()[0], start, time.Now())
}

func TestScheduler_ContinuesAfterError(t *testing.T) {
	repo := &fakeLinkRepository{deleteErr: errors.New("база недоступна")}
	s := NewScheduler(repo, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	require.Eventually(t, func() bool {
		return repo.callCount() >= 3
	}, time.Second, 5*time.Millisecond)
}

func TestScheduler_StopHaltsTicker(t *testing.T) {
	repo := &fakeLinkRepository{}
	s := NewScheduler(repo, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	require.Eventually(t, func() bool {
		return repo.callCount() >= 1
	}, time.Second, 5*time.Millisecond)

	s.Stop() // возвращается только после выхода Run, дальше вызовов быть не может

	countAfterStop := repo.callCount()

	// Sleep, а не Eventually: тест проверяет, что событий НЕ будет после Stop,
	// а Eventually проверяет наступление события и здесь не подходит
	// Пауза на порядок больше интервала, чтобы ошибочный тикер гарантированно успел сработать
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, countAfterStop, repo.callCount())
}

func TestScheduler_StopBeforeFirstTick(t *testing.T) {
	repo := &fakeLinkRepository{}
	s := NewScheduler(repo, time.Hour) // первый тик заведомо не наступит

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	stopped := make(chan struct{})
	go func() {
		s.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop заблокировался, пока планировщик ждет первый тик")
	}

	assert.Zero(t, repo.callCount())
}

func TestScheduler_RunReturnsOnContextCancel(t *testing.T) {
	repo := &fakeLinkRepository{}
	s := NewScheduler(repo, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())

	finished := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(finished)
	}()

	cancel()

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Run не завершился после отмены контекста")
	}
}
