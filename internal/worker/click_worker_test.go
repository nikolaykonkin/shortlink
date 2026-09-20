package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nikolaykonkin/shortlink/internal/model"
	"github.com/nikolaykonkin/shortlink/internal/repository"
)

// fakeClickRepository — in-memory реализация repository.ClickRepository, потокобезопасная:
// CreateBatch пишет из горутины воркера, allLinkIDs читает из горутины теста
type fakeClickRepository struct {
	mu      sync.Mutex
	batches [][]int64
}

func (f *fakeClickRepository) Create(_ context.Context, _ *model.Click) error {
	return errors.New("не используется в этих тестах")
}

func (f *fakeClickRepository) CreateBatch(_ context.Context, linkIDs []int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	batch := make([]int64, len(linkIDs))
	copy(batch, linkIDs)
	f.batches = append(f.batches, batch)

	return nil
}

func (f *fakeClickRepository) CountByLinkID(_ context.Context, _ int64) (int64, error) {
	return 0, errors.New("не используется в этих тестах")
}

func (f *fakeClickRepository) allLinkIDs() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()

	var all []int64
	for _, batch := range f.batches {
		all = append(all, batch...)
	}

	return all
}

var _ repository.ClickRepository = (*fakeClickRepository)(nil)

func TestClickWorker_FlushesOnBatchSize(t *testing.T) {
	repo := &fakeClickRepository{}
	w := NewClickWorker(repo, 5, time.Hour) // интервал заведомо больше теста — сработать должен только размер батча

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	for i := int64(1); i <= 5; i++ {
		w.Record(i)
	}

	require.Eventually(t, func() bool {
		return len(repo.allLinkIDs()) == 5
	}, time.Second, 10*time.Millisecond)

	assert.ElementsMatch(t, []int64{1, 2, 3, 4, 5}, repo.allLinkIDs())
}

func TestClickWorker_FlushesOnTicker(t *testing.T) {
	repo := &fakeClickRepository{}
	w := NewClickWorker(repo, 100, 20*time.Millisecond) // батч заведомо больше — сработать должен только тикер

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	w.Record(42)

	require.Eventually(t, func() bool {
		return len(repo.allLinkIDs()) == 1
	}, time.Second, 10*time.Millisecond)

	assert.Equal(t, []int64{42}, repo.allLinkIDs())
}

func TestClickWorker_StopDrainsBuffer(t *testing.T) {
	repo := &fakeClickRepository{}
	w := NewClickWorker(repo, 100, time.Hour) // ни батч, ни тикер сами не успеют сработать

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	w.Record(1)
	w.Record(2)
	w.Record(3)

	w.Stop() // возвращается только после того, как воркер слил буфер

	assert.ElementsMatch(t, []int64{1, 2, 3}, repo.allLinkIDs())
}

func TestClickWorker_StopWithEmptyBuffer(t *testing.T) {
	repo := &fakeClickRepository{}
	w := NewClickWorker(repo, 100, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	w.Stop()

	assert.Empty(t, repo.batches)
}
