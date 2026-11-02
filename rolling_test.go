package glogs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRollingWriterRotatesBySize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app", "app.log")
	w, err := newRollingWriter(path, 32, 7, 1<<20)
	if err != nil {
		t.Fatalf("newRollingWriter: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := w.Write([]byte("0123456789")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	var rotated int
	for _, e := range entries {
		if strings.Contains(e.Name(), ".20") && strings.HasSuffix(e.Name(), ".log") {
			rotated++
		}
	}
	if rotated == 0 {
		t.Fatalf("expected rotated files, entries: %v", entries)
	}
}

func TestRollingWriterRetention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app", "app.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(filepath.Dir(path), "app.2000-01-01.0.log")
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := newRollingWriter(path, 1<<20, 7, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old rotated file should be removed")
	}
}

// A daily rollover names the rotated file after the day that just ended: a consumer
// tailing app.YYYY-MM-DD.* must not find today's writes in yesterday's file.
func TestRollingWriterRotatesOnDateChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	now := time.Date(2026, 10, 31, 23, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	w, err := newRollingWriterWith(path, 0, 7, 1<<20, withClock(clock))
	if err != nil {
		t.Fatalf("newRollingWriter: %v", err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("yesterday\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	now = now.Add(2 * time.Hour) // same writer, next day
	if _, err := w.Write([]byte("today\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	rotated := filepath.Join(dir, "app.2026-10-31.0.log")
	content, err := os.ReadFile(rotated)
	if err != nil {
		t.Fatalf("rotated file: %v", err)
	}
	if string(content) != "yesterday\n" {
		t.Fatalf("rotated content = %q, want the previous day's writes", content)
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("current file: %v", err)
	}
	if string(current) != "today\n" {
		t.Fatalf("current content = %q, want only today's write", current)
	}
}
