package glogs

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	cfg := DefaultConfig("xdmp-test")
	cfg.Dir = dir
	for name, cc := range cfg.Channels {
		cc.Path = filepath.Join(dir, "common", string(name), string(name)+".log")
		cfg.Channels[name] = cc
	}
	return cfg
}

func TestChannelsWriteFiles(t *testing.T) {
	cfg := testConfig(t)
	l, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.Root().Info("root message")
	l.Channel(ChannelApp).Info("app message", "k", "v")
	l.Channel(ChannelError).Error("error message")
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, ch := range []Channel{ChannelApp, ChannelError} {
		b, err := os.ReadFile(cfg.Channels[ch].Path)
		if err != nil {
			t.Fatalf("read %s: %v", ch, err)
		}
		if !strings.Contains(string(b), "["+string(ch)+"]") {
			t.Fatalf("channel %s missing in log: %s", ch, b)
		}
	}
}

func TestWithFields(t *testing.T) {
	cfg := testConfig(t)
	l, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()
	ctx := WithFields(t.Context(), slog.String("trace_id", "abc123"))
	l.Channel(ChannelDefault).InfoContext(ctx, "with context")
	b, _ := os.ReadFile(cfg.Channels[ChannelDefault].Path)
	if !strings.Contains(string(b), "trace_id=abc123") {
		t.Fatalf("context field missing: %s", b)
	}
}

func TestStat(t *testing.T) {
	cfg := testConfig(t)
	l, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()
	l.Stat().Add("event", "access").Add("code", 200).Log(l.Channel(ChannelEvent))
	b, _ := os.ReadFile(cfg.Channels[ChannelEvent].Path)
	if !strings.Contains(string(b), "`event=access") || !strings.Contains(string(b), "`code=200") {
		t.Fatalf("stat format mismatch: %s", b)
	}
}
