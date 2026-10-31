package glogs

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestTextHandlerFormat(t *testing.T) {
	var buf bytes.Buffer
	h := newHandler(&buf, FormatText, false, slog.LevelInfo, "app", nil)
	lg := slog.New(h)
	lg.Info("hello", "k", "v")
	out := buf.String()
	for _, want := range []string{"INFO", "[app]", "hello", "k=v"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func TestTextHandlerColor(t *testing.T) {
	var buf bytes.Buffer
	h := newHandler(&buf, FormatText, true, slog.LevelInfo, "app", nil)
	slog.New(h).Error("boom")
	if !strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("expected ANSI colors, got %q", buf.String())
	}
}

func TestJSONHandler(t *testing.T) {
	var buf bytes.Buffer
	h := newHandler(&buf, FormatJSON, false, slog.LevelInfo, "access", nil)
	slog.New(h).Info("json", "status", 200)
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("invalid json: %v (%s)", err, buf.String())
	}
	if m["channel"] != "access" || m["message"] != "json" {
		t.Fatalf("unexpected json: %v", m)
	}
}
