package glogs

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// breakFile makes the next write fail without touching the filesystem: the writer
// is left holding a read-only descriptor.
func breakFile(t *testing.T, w *rollingWriter) {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()

	readOnly, err := os.Open(w.path)
	if err != nil {
		t.Fatalf("open read-only: %v", err)
	}
	_ = w.file.Close()
	w.file = readOnly
}

// slog discards the error returned by a handler, so a failing disk used to lose
// lines with no signal at all. The counters and the hook are that signal.
func TestRollingWriterRecordsWriteFailures(t *testing.T) {
	dir := t.TempDir()
	writer, err := newRollingWriter(filepath.Join(dir, "app.log"), 1<<20, 7, 1<<20)
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	defer writer.Close()

	breakFile(t, writer)

	if _, err := writer.Write([]byte("line\n")); err == nil {
		t.Fatal("write to a read-only descriptor must fail")
	}

	stats := writer.stats()
	if stats.Errors == 0 {
		t.Fatal("failure was not counted")
	}
	if stats.LastErr == "" {
		t.Fatal("last error not recorded")
	}
}

// The hook is what a deployment wires to its alerting.
func TestOnErrorHookSeesChannelFailures(t *testing.T) {
	dir := t.TempDir()

	var mu sync.Mutex
	var seen []string
	logger, err := New(Config{
		AppName: "fail",
		Dir:     dir,
		Root:    ConsoleConfig{Enabled: false},
		Channels: map[Channel]ChannelConfig{
			ChannelApp: {Enabled: true, Path: filepath.Join(dir, "app.log"), Console: false},
		},
		OnError: func(channel Channel, err error) {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, string(channel)+": "+err.Error())
		},
	})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	defer logger.Close()

	breakFile(t, logger.files[ChannelApp])

	// slog ignores the handler error, which is precisely why the hook exists.
	logger.Channel(ChannelApp).Info("this line cannot be written")

	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("OnError was not called for a failed write")
	}
	if !strings.HasPrefix(seen[0], string(ChannelApp)+": ") {
		t.Fatalf("hook reported %q, want the channel name first", seen[0])
	}

	stats, err := logger.FileStats(ChannelApp)
	if err != nil {
		t.Fatalf("file stats: %v", err)
	}
	if stats.Errors == 0 {
		t.Fatal("FileStats did not count the failure")
	}
}

func TestFileStatsRejectsLocalOnlyChannels(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(Config{
		AppName: "nostats",
		Dir:     dir,
		Root:    ConsoleConfig{Enabled: false},
		Channels: map[Channel]ChannelConfig{
			ChannelApp: {Enabled: true, Path: filepath.Join(dir, "app.log")},
		},
	})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	defer logger.Close()

	if _, err := logger.FileStats(ChannelError); err == nil {
		t.Fatal("a channel without a file must not report file stats")
	}
	stats, err := logger.FileStats(ChannelApp)
	if err != nil {
		t.Fatalf("file stats: %v", err)
	}
	if stats.Errors != 0 {
		t.Fatalf("a fresh writer reported %d errors", stats.Errors)
	}
}

// An async channel forwards failures too: the async writer reports the write error
// to the same hook through the rolling writer underneath.
func TestAsyncChannelRecordsFileFailures(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(Config{
		AppName: "asyncfail",
		Dir:     dir,
		Root:    ConsoleConfig{Enabled: false},
		Channels: map[Channel]ChannelConfig{
			ChannelApp: {Enabled: true, Async: true, AsyncBuffer: 8, Path: filepath.Join(dir, "app.log")},
		},
	})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	breakFile(t, logger.files[ChannelApp])
	logger.Channel(ChannelApp).Info("lost line")

	if err := logger.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	stats, err := logger.FileStats(ChannelApp)
	if err != nil {
		t.Fatalf("file stats: %v", err)
	}
	if stats.Errors == 0 {
		t.Fatal("the async path hid a file failure")
	}
}

var _ = slog.LevelInfo
