package usage

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

const flushTimeout = 5 * time.Second

type inserter interface {
	Insert(ctx context.Context, records []Record) error
}

// Writer saves usage records in the background, in batches. The handlers
// hand records over with Send, so a slow database does not slow a request.
type Writer struct {
	store     inserter
	records   chan Record
	stop      chan struct{}
	done      chan struct{}
	dropped   atomic.Int64
	batchSize int
	interval  time.Duration
}

func NewWriter(store inserter, bufferSize, batchSize int, interval time.Duration) *Writer {
	return &Writer{
		store:     store,
		records:   make(chan Record, bufferSize),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		batchSize: batchSize,
		interval:  interval,
	}
}

// Send hands r to the writer without waiting. If the buffer is full, r is
// dropped and counted, so a slow database never delays a request.
func (w *Writer) Send(r Record) {
	select {
	case w.records <- r:
	default:
		w.dropped.Add(1)
	}
}

// flush writes batch and returns it with length 0, so the caller can reuse
// its memory. The reuse is safe only because Insert does not keep the slice.
// The context does not come from the signal context, which is already
// canceled at shutdown, when the last batch is written.
func (w *Writer) flush(batch []Record) []Record {
	if n := w.dropped.Swap(0); n > 0 {
		slog.Warn("usage records dropped, buffer full", "count", n)
	}
	if len(batch) == 0 {
		return batch
	}

	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	if err := w.store.Insert(ctx, batch); err != nil {
		slog.Error("writing usage records failed", "count", len(batch), "err", err)
	}
	return batch[:0]
}

// Run writes records until Close is called. Start it once, in its own
// goroutine. It takes no context on purpose: requests still send records
// while the server shuts down, after the signal context is canceled.
func (w *Writer) Run() {
	defer close(w.done)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	batch := make([]Record, 0, w.batchSize)
	for {
		select {
		case r := <-w.records:
			batch = append(batch, r)
			if len(batch) >= w.batchSize {
				batch = w.flush(batch)
			}
		case <-ticker.C:
			batch = w.flush(batch)
		case <-w.stop:
			// select picks a random ready case, so records can still be
			// waiting in the channel. Write them all before the return.
			for {
				select {
				case r := <-w.records:
					batch = append(batch, r)
					if len(batch) >= w.batchSize {
						batch = w.flush(batch)
					}
				default:
					w.flush(batch)
					return
				}
			}
		}
	}
}
