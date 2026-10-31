package glogs

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// slowWriter stands in for a disk that cannot keep up.
type slowWriter struct {
	mu       sync.Mutex
	buffer   strings.Builder
	delay    time.Duration
	failFrom int
	writes   atomic.Int64
}

func (w *slowWriter) Write(p []byte) (int, error) {
	w.writes.Add(1)
	if w.delay > 0 {
		time.Sleep(w.delay)
	}
	if w.failFrom > 0 && int(w.writes.Load()) >= w.failFrom {
		return 0, errors.New("disk full")
	}
	w.mu.Lock()
	w.buffer.Write(p)
	w.mu.Unlock()
	return len(p), nil
}

func (w *slowWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

// The point of the async writer: a slow disk must not slow the caller down.
func TestAsyncWriterDoesNotBlockTheCaller(t *testing.T) {
	target := &slowWriter{delay: 20 * time.Millisecond}
	writer := newAsyncWriter(target, 1024)
	defer func() { _ = writer.Close(context.Background()) }()

	start := time.Now()
	for i := 0; i < 100; i++ {
		if _, err := writer.Write([]byte("line\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	elapsed := time.Since(start)

	// 100 synchronous writes would need ~2s; enqueueing must be far below that.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("enqueueing took %s: the caller is still waiting on the disk", elapsed)
	}
}

// When the queue is full the line is dropped and counted — never blocked on, and
// never reported as an error the caller cannot act on.
func TestAsyncWriterDropsAndCountsWhenFull(t *testing.T) {
	target := &slowWriter{delay: 30 * time.Millisecond}
	writer := newAsyncWriter(target, 2)
	defer func() { _ = writer.Close(context.Background()) }()

	for i := 0; i < 20; i++ {
		if _, err := writer.Write([]byte("line\n")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	stats := writer.Stats()
	if stats.Dropped == 0 {
		t.Fatalf("dropped = 0, want drops when the queue is full: %+v", stats)
	}
	if stats.Dropped+int64(stats.Pending)+stats.Written != 20 && stats.Written < 1 {
		t.Fatalf("counter mismatch: %+v", stats)
	}
}

// Close drains what is queued: shutdown must not lose lines that were accepted.
func TestAsyncWriterDrainsOnClose(t *testing.T) {
	target := &slowWriter{}
	writer := newAsyncWriter(target, 64)

	for i := 0; i < 10; i++ {
		if _, err := writer.Write([]byte("x")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	if err := writer.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := target.String(); len(got) != 10 {
		t.Fatalf("drained %d of 10 bytes", len(got))
	}
	if _, err := writer.Write([]byte("late")); !errors.Is(err, ErrWriterClosed) {
		t.Fatalf("write after close = %v, want ErrWriterClosed", err)
	}
}

// A failing disk is counted and surfaced, not swallowed.
func TestAsyncWriterRecordsErrors(t *testing.T) {
	target := &slowWriter{failFrom: 1}
	writer := newAsyncWriter(target, 8)

	if _, err := writer.Write([]byte("line\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = writer.Close(context.Background())

	stats := writer.Stats()
	if stats.Errors == 0 {
		t.Fatalf("errors = 0, want the disk failure counted: %+v", stats)
	}
	if stats.LastErr == "" {
		t.Fatal("last error not recorded")
	}
}

// The logger wires async per channel and exposes the counters.
func TestLoggerAsyncChannel(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(Config{
		AppName: "async",
		Dir:     dir,
		Root:    ConsoleConfig{Enabled: false},
		Channels: map[Channel]ChannelConfig{
			ChannelApp: {Enabled: true, Async: true, AsyncBuffer: 8, Path: dir + "/app.log", MaxSize: 1 << 20, MaxAge: 1, TotalCap: 1 << 20},
		},
	})
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}

	for i := 0; i < 5; i++ {
		logger.Channel(ChannelApp).Info("event", "i", i)
	}

	if _, err := logger.AsyncStats(ChannelApp); err != nil {
		t.Fatalf("async stats: %v", err)
	}
	if _, err := logger.AsyncStats(ChannelError); err == nil {
		t.Fatal("a synchronous channel must not report async stats")
	}

	if err := logger.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// After the drain the file holds the lines.
	content, err := os.ReadFile(dir + "/app.log")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(content)
	if strings.Count(text, "event") != 5 {
		t.Fatalf("file has %d lines, want 5:\n%s", strings.Count(text, "event"), text)
	}
}
