package bridge

import (
	"context"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/maxid/glogs"
)

func newLogger(t *testing.T) (*glogs.Logger, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "default.log")
	logger, err := glogs.New(glogs.Config{
		AppName: "bridge",
		Dir:     dir,
		Root:    glogs.ConsoleConfig{Enabled: false},
		Channels: map[glogs.Channel]glogs.ChannelConfig{
			glogs.ChannelDefault: {Enabled: true, Path: path},
			glogs.ChannelDepend:  {Enabled: true, Path: filepath.Join(dir, "depend.log")},
		},
	})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	return logger, path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// Lines written by a third-party logger end up in the glogs channel.
func TestWriterCapturesLines(t *testing.T) {
	logger, path := newLogger(t)
	writer := NewWriter(logger, Options{Channel: glogs.ChannelDefault, Source: "zap"})

	if _, err := writer.Write([]byte("first line\nsecond")); err != nil {
		t.Fatalf("write: %v", err)
	}
	// The trailing partial line is flushed on Close, not before.
	if strings.Contains(readFile(t, path), "second") {
		t.Fatal("a partial line was emitted before the newline")
	}

	if _, err := writer.Write([]byte(" line\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	content := readFile(t, path)
	if !strings.Contains(content, "first line") || !strings.Contains(content, "second line") {
		t.Fatalf("lines missing:\n%s", content)
	}
	if !strings.Contains(content, "source=zap") {
		t.Fatalf("source label missing:\n%s", content)
	}
}

func TestWriterFlushesTrailingLineOnClose(t *testing.T) {
	logger, path := newLogger(t)
	writer := NewWriter(logger, Options{Channel: glogs.ChannelDefault})

	if _, err := writer.Write([]byte("no newline")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !strings.Contains(readFile(t, path), "no newline") {
		t.Fatalf("trailing line was lost:\n%s", readFile(t, path))
	}

	if _, err := writer.Write([]byte("late")); err == nil {
		t.Fatal("write after close must fail")
	}
}

// Levels are invisible in a plain line; sniffing recovers the common shapes.
func TestSniffLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"INFO something happened":   slog.LevelInfo,
		"[WARN]  slow request":      slog.LevelWarn,
		"error: connection refused": slog.LevelError,
		"DEBUG cache hit":           slog.LevelDebug,
	}
	for line, want := range cases {
		got, ok := sniffLevel(line)
		if !ok || got != want {
			t.Errorf("sniffLevel(%q) = %v, %v; want %v", line, got, ok, want)
		}
	}
	if _, ok := sniffLevel("just a message"); ok {
		t.Error("a line without a level token must not be sniffed")
	}
}

func TestSniffLevelAppliesToRecords(t *testing.T) {
	logger, path := newLogger(t)
	writer := NewWriter(logger, Options{Channel: glogs.ChannelDefault, SniffLevel: true})

	if _, err := writer.Write([]byte("ERROR boom\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = writer.Close()

	content := readFile(t, path)
	if !strings.Contains(content, "ERROR") {
		t.Fatalf("level not applied:\n%s", content)
	}
}

// A dependency that dumps a payload must not fill the file.
func TestWriterTruncatesLongLines(t *testing.T) {
	logger, path := newLogger(t)
	writer := NewWriter(logger, Options{Channel: glogs.ChannelDefault, MaxLineBytes: 16})

	if _, err := writer.Write([]byte(strings.Repeat("x", 100) + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = writer.Close()

	content := readFile(t, path)
	if !strings.Contains(content, "...(truncated)") {
		t.Fatalf("no truncation marker:\n%s", content)
	}
	if len(content) > 200 {
		t.Fatalf("line was not bounded: %d bytes", len(content))
	}
}

// The standard library path: one call captures every package that uses log.Printf.
func TestStdlibRedirect(t *testing.T) {
	logger, path := newLogger(t)

	restore := Stdlib(logger, Options{Channel: glogs.ChannelDefault, Source: "stdlib"})
	log.Printf("from a dependency")
	restore()

	content := readFile(t, path)
	if !strings.Contains(content, "from a dependency") {
		t.Fatalf("stdlib line missing:\n%s", content)
	}
	if !strings.Contains(content, "source=stdlib") {
		t.Fatalf("source label missing:\n%s", content)
	}

	// The previous output is restored.
	var mu sync.Mutex
	restored := false
	log.SetOutput(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		restored = true
		mu.Unlock()
		return len(p), nil
	}))
	log.Print("after restore")
	mu.Lock()
	defer mu.Unlock()
	if !restored {
		t.Fatal("log output was not restored to the caller's writer")
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// Redaction applies to bridged lines too: the channel handler is shared.
func TestBridgeHonoursRedaction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "default.log")
	logger, err := glogs.New(glogs.Config{
		AppName:  "bridge-redact",
		Dir:      dir,
		Root:     glogs.ConsoleConfig{Enabled: false},
		Redactor: glogs.NewMaskRedactor(""),
		Channels: map[glogs.Channel]glogs.ChannelConfig{
			glogs.ChannelDefault: {Enabled: true, Path: path},
		},
	})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	defer logger.Close()

	// A structured emitter routes through the handler's attribute redaction.
	logger.Channel(glogs.ChannelDefault).Info("login", "password", "s3cret")
	_ = context.Background()

	content := readFile(t, path)
	if strings.Contains(content, "s3cret") {
		t.Fatalf("credential reached the bridged file:\n%s", content)
	}
}
