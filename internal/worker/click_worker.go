package worker

import (
	"context"
	"log"
	"time"

	"github.com/nikolaykonkin/shortlink/internal/repository"
)

const (
	clickChannelBuffer = 256
	clickBatchSize     = 50
	clickFlushInterval = 5 * time.Second
)

// ClickWorker асинхронно накапливает клики и пишет их в БД пачками,
// чтобы редирект не ждал вставки в базу
type ClickWorker struct {
	clicks repository.ClickRepository
	queue  chan int64
	done   chan struct{}
}

// NewClickWorker создает воркер вместе с каналом приема кликов
func NewClickWorker(clicks repository.ClickRepository) *ClickWorker {
	return &ClickWorker{
		clicks: clicks,
		queue:  make(chan int64, clickChannelBuffer),
		done:   make(chan struct{}),
	}
}

// Record ставит linkID в очередь на запись
func (w *ClickWorker) Record(linkID int64) {
	w.queue <- linkID
}

// Run читает очередь до закрытия канала, сбрасывая накопленное в БД
// по размеру батча или по таймеру, смотря что наступит раньше
func (w *ClickWorker) Run(ctx context.Context) {
	defer close(w.done)

	ticker := time.NewTicker(clickFlushInterval)
	defer ticker.Stop()

	batch := make([]int64, 0, clickBatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := w.clicks.CreateBatch(ctx, batch); err != nil {
			log.Printf("запись батча кликов: %v", err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case linkID, ok := <-w.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, linkID)
			if len(batch) >= clickBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// Stop закрывает канал приема и ждет, пока воркер сольет накопленный буфер — вызывать только
// после остановки HTTP-сервера, иначе Record из еще идущего запроса попадет в закрытый канал
func (w *ClickWorker) Stop() {
	close(w.queue)
	<-w.done
}
