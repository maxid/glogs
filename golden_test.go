package glogs

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The JSON field names are a contract: log consumers index on them. A rename
// here silently breaks dashboards, so the shape is pinned.
func TestGoldenJSONShape(t *testing.T) {
	var buf bytes.Buffer
	handler := newHandler(&buf, FormatJSON, false, slog.LevelInfo, "app", nil)

	record := slog.NewRecord(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), slog.LevelInfo, "login", 0)
	record.AddAttrs(slog.String("username", "alice"), slog.Int("status", 200))
	if err := handler.Handle(nil, record); err != nil {
		t.Fatalf("handle: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not JSON: %v (%s)", err, buf.String())
	}

	// Renamed keys.
	if decoded["ts"] != "2026-10-31T12:00:00.000Z" {
		t.Errorf("ts = %v", decoded["ts"])
	}
	if decoded["message"] != "login" {
		t.Errorf("message = %v", decoded["message"])
	}
	// Added key.
	if decoded["channel"] != "app" {
		t.Errorf("channel = %v", decoded["channel"])
	}
	// Standard slog keys.
	if decoded["level"] != "INFO" {
		t.Errorf("level = %v", decoded["level"])
	}
	if decoded["username"] != "alice" || decoded["status"] != float64(200) {
		t.Errorf("attributes lost: %v", decoded)
	}
	// The default slog names must not leak.
	if _, ok := decoded["time"]; ok {
		t.Error(`"time" must be renamed to "ts"`)
	}
	if _, ok := decoded["msg"]; ok {
		t.Error(`"msg" must be renamed to "message"`)
	}
}

// The text line layout is what operators grep for.
func TestGoldenTextLine(t *testing.T) {
	var buf bytes.Buffer
	handler := newHandler(&buf, FormatText, false, slog.LevelInfo, "event", nil)

	record := slog.NewRecord(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC), slog.LevelWarn, "device online", 0)
	record.AddAttrs(slog.String("key", "v"))
	if err := handler.Handle(nil, record); err != nil {
		t.Fatalf("handle: %v", err)
	}

	line := buf.String()
	pattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3} WARN\s+\[event\] device online key=v\n$`)
	if !pattern.MatchString(line) {
		t.Fatalf("line = %q, does not match the documented layout", line)
	}
}

// Rotated file names carry date and index; a consumer tailing them depends on it.
func TestGoldenRollingNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	writer, err := newRollingWriter(path, 10, 7, 1<<20)
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	defer writer.Close()

	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	writer.now = func() time.Time { return now }

	// Exceed maxSize so the file rotates.
	if _, err := writer.Write([]byte("0123456789abc")); err != nil {
		t.Fatalf("write: %v", err)
	}

	rotated, err := filepath.Glob(filepath.Join(dir, "app.*.log"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(rotated) == 0 {
		t.Fatalf("no rotated file; dir contains %v", mustList(t, dir))
	}
	name := filepath.Base(rotated[0])
	pattern := regexp.MustCompile(`^app\.\d{4}-\d{2}-\d{2}\.\d+\.log$`)
	if !pattern.MatchString(name) {
		t.Fatalf("rotated name = %q, does not match app.YYYY-MM-DD.index.log", name)
	}
	if !strings.HasPrefix(name, "app.2026-10-31.") {
		t.Fatalf("rotated name = %q, want the injected date", name)
	}
}

func mustList(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return nil
	}
	return matches
}
