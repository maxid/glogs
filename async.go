package glogs

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// ErrWriterClosed means a write arrived after Close.
var ErrWriterClosed = errors.New("glogs: writer is closed")

// AsyncStats reports what an async writer did. Dropping is lossy by design: a log
// line must never block a request goroutine, and the queue is bounded so memory
// cannot grow without limit. The counter is how that trade-off stays visible.
type AsyncStats struct {
	Written int64
	Dropped int64
	Errors  int64
	Pending int
	LastErr string
}

// asyncWriter decouples the caller from the disk: Write enqueues a copy and
// returns, one goroutine drains the queue into the target writer.
//
// The bound is a hard limit, not a hint. When the queue is full the line is
// dropped and counted instead of blocking: losing a log line is better than
// stalling the request that produced it.
type asyncWriter struct {
	target io.Writer
	queue  chan []byte
	done   chan struct{}

	wg        sync.WaitGroup
	closeOnce sync.Once
	closed    atomic.Bool

	written atomic.Int64
	dropped atomic.Int64
	errors  atomic.Int64
	lastErr atomic.Pointer[string]
}

// newAsyncWriter starts the drain goroutine.
func newAsyncWriter(target io.Writer, buffer int) *asyncWriter {
	if buffer <= 0 {
		buffer = 1024
	}
	w := &asyncWriter{
		target: target,
		queue:  make(chan []byte, buffer),
		done:   make(chan struct{}),
	}
	w.wg.Add(1)
	go w.drain()
	return w
}

// drain moves queued lines into the target until the queue is closed and empty.
func (w *asyncWriter) drain() {
	defer w.wg.Done()

	for line := range w.queue {
		if _, err := w.target.Write(line); err != nil {
			w.errors.Add(1)
			message := err.Error()
			w.lastErr.Store(&message)
			// A failing disk must not stop the drain: dropping the rest silently
			// would hide the outage, and blocking would break the caller contract.
			continue
		}
		w.written.Add(1)
	}
	_ = w.done
}

// Write copies and enqueues the line. It never blocks on the disk.
func (w *asyncWriter) Write(p []byte) (int, error) {
	if w.closed.Load() {
		return 0, ErrWriterClosed
	}

	line := make([]byte, len(p))
	copy(line, p)

	select {
	case w.queue <- line:
		return len(p), nil
	default:
		// Full: drop this line, count it, and report success. Returning an error
		// would make slog retry or panic on a condition the caller cannot fix.
		w.dropped.Add(1)
		return len(p), nil
	}
}

// Close stops accepting writes, drains what is queued, and does not close the
// target (the caller owns it). It waits at most until ctx is done.
func (w *asyncWriter) Close(ctx context.Context) error {
	w.closeOnce.Do(func() {
		w.closed.Store(true)
		close(w.queue)
	})

	finished := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stats snapshots the counters.
func (w *asyncWriter) Stats() AsyncStats {
	stats := AsyncStats{
		Written: w.written.Load(),
		Dropped: w.dropped.Load(),
		Errors:  w.errors.Load(),
		Pending: len(w.queue),
	}
	if last := w.lastErr.Load(); last != nil {
		stats.LastErr = *last
	}
	return stats
}

// asyncDrainTimeout bounds how long Close waits for the queue.
const asyncDrainTimeout = 5 * time.Second
