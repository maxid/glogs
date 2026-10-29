package glogs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
