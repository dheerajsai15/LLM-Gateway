package usage

import (
	"context"
	"sync/atomic"
	"time"
)

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
