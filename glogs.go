// Package glogs is a small structured logging facade for Go services.
//
// It provides named channels, per-channel rolling files, ANSI colored console
// output, structured text/JSON output and request context fields.
package glogs

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Channel is a named log channel.
type Channel string

const (
	ChannelAccess  Channel = "access"
	ChannelDefault Channel = "default"
	ChannelDepend  Channel = "depend"
	ChannelApp     Channel = "app"
	ChannelError   Channel = "error"
	ChannelEvent   Channel = "event"
)

// DefaultChannels is the default channel set.
var DefaultChannels = []Channel{
	ChannelAccess, ChannelDefault, ChannelDepend, ChannelApp, ChannelError, ChannelEvent,
}

// Format is the output format.
type Format int

const (
	FormatText Format = iota
	FormatJSON
)

// ColorMode controls ANSI colors on the console.
type ColorMode int

const (
	ColorAuto ColorMode = iota
	ColorOn
	ColorOff
)

// ConsoleConfig configures console output.
type ConsoleConfig struct {
	Enabled bool
	Format  Format
	Color   ColorMode
}

// ChannelConfig configures one channel's file output and rotation.
type ChannelConfig struct {
	Enabled  bool
	Level    slog.Level
	Path     string
	Console  bool
	MaxSize  int64 // bytes per file, default 50 MiB
	MaxAge   int   // days, default 7
	TotalCap int64 // bytes, default 500 MiB

	// Async moves the file write off the caller's goroutine. The queue is
	// bounded by AsyncBuffer; when it is full the line is dropped and counted
	// rather than blocking the request. Off by default so an existing deployment
	// keeps its current durability.
	Async bool
	// AsyncBuffer is the queue depth when Async is set. Default 1024.
	AsyncBuffer int
}

// Config is the top-level logging configuration.
type Config struct {
	AppName  string
	Dir      string // base directory, default "./logs"
	Root     ConsoleConfig
	Level    slog.Level
	Channels map[Channel]ChannelConfig

	// Redactor masks sensitive attributes and bodies. Nil disables redaction,
	// which is only appropriate for trusted local debugging: a logged credential
	// lives in the file forever.
	Redactor Redactor

	// OnError is told about every file write, rotation and cleanup failure.
	// slog discards handler errors, so this is the only way a broken disk becomes
	// visible instead of silently dropping log lines.
	OnError func(channel Channel, err error)
}

// DefaultConfig returns a configuration matching the platform defaults.
func DefaultConfig(appName string) Config {
	if appName == "" {
		appName = "glogs"
	}
	base := filepath.Join("logs", appName, "common")
	return Config{
		AppName: appName,
		Dir:     "logs",
		Root:    ConsoleConfig{Enabled: true, Format: FormatText, Color: ColorAuto},
		Level:   slog.LevelInfo,
		Channels: map[Channel]ChannelConfig{
			ChannelAccess:  {Enabled: true, Level: slog.LevelInfo, Path: filepath.Join(base, "access", "access.log"), Console: true, MaxSize: 50 << 20, MaxAge: 7, TotalCap: 500 << 20},
			ChannelDefault: {Enabled: true, Level: slog.LevelInfo, Path: filepath.Join(base, "default", "default.log"), MaxSize: 50 << 20, MaxAge: 7, TotalCap: 500 << 20},
			ChannelDepend:  {Enabled: true, Level: slog.LevelInfo, Path: filepath.Join(base, "depend", "depend.log"), MaxSize: 50 << 20, MaxAge: 7, TotalCap: 500 << 20},
			ChannelApp:     {Enabled: true, Level: slog.LevelInfo, Path: filepath.Join(base, "app", "app.log"), MaxSize: 50 << 20, MaxAge: 7, TotalCap: 500 << 20},
			ChannelError:   {Enabled: true, Level: slog.LevelInfo, Path: filepath.Join(base, "error", "error.log"), MaxSize: 50 << 20, MaxAge: 7, TotalCap: 500 << 20},
			ChannelEvent:   {Enabled: true, Level: slog.LevelInfo, Path: filepath.Join(base, "event", "event.log"), MaxSize: 50 << 20, MaxAge: 7, TotalCap: 500 << 20},
		},
	}
}

// Logger holds the root console logger and the named channel loggers.
type Logger struct {
	root     *slog.Logger
	channels map[Channel]*slog.Logger
	closers  []io.Closer
	async    map[Channel]*asyncWriter
	files    map[Channel]*rollingWriter
	mu       sync.Mutex
}

// New builds a Logger from cfg.
func New(cfg Config) (*Logger, error) {
	if cfg.Dir == "" {
		cfg.Dir = "logs"
	}
	if cfg.AppName == "" {
		cfg.AppName = "glogs"
	}
	if cfg.Level == 0 {
		cfg.Level = slog.LevelInfo
	}
	l := &Logger{
		channels: make(map[Channel]*slog.Logger),
		async:    map[Channel]*asyncWriter{},
		files:    map[Channel]*rollingWriter{},
	}

	if cfg.Root.Enabled {
		color := consoleColor(cfg.Root.Color)
		h := newHandler(os.Stdout, cfg.Root.Format, color, cfg.Level, "root", cfg.Redactor)
		l.root = slog.New(h)
	} else {
		l.root = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: cfg.Level}))
	}

	if len(cfg.Channels) == 0 {
		cfg.Channels = DefaultConfig(cfg.AppName).Channels
	}
	for name, cc := range cfg.Channels {
		if !cc.Enabled {
			continue
		}
		if cc.Path == "" {
			cc.Path = filepath.Join(cfg.Dir, cfg.AppName, "common", string(name), string(name)+".log")
		}
		if cc.MaxSize <= 0 {
			cc.MaxSize = 50 << 20
		}
		if cc.MaxAge <= 0 {
			cc.MaxAge = 7
		}
		if cc.TotalCap <= 0 {
			cc.TotalCap = 500 << 20
		}
		if cc.Level == 0 {
			cc.Level = cfg.Level
		}
		rw, err := newRollingWriter(cc.Path, cc.MaxSize, cc.MaxAge, cc.TotalCap)
		if err != nil {
			_ = l.Close()
			return nil, fmt.Errorf("glogs: init channel %s: %w", name, err)
		}
		l.closers = append(l.closers, rw)
		if cfg.OnError != nil {
			channel := name
			rw.onError = func(err error) { cfg.OnError(channel, err) }
		}
		l.files[name] = rw

		var sink io.Writer = rw
		if cc.Async {
			w := newAsyncWriter(rw, cc.AsyncBuffer)
			l.async[name] = w
			sink = w
		}
		fileHandler := newHandler(sink, cfg.Root.Format, false, cc.Level, string(name), cfg.Redactor)
		var h slog.Handler = fileHandler
		if cc.Console {
			consoleHandler := newHandler(os.Stdout, cfg.Root.Format, consoleColor(cfg.Root.Color), cc.Level, string(name), cfg.Redactor)
			h = &multiHandler{handlers: []slog.Handler{fileHandler, consoleHandler}}
		}
		l.channels[name] = slog.New(h)
	}
	return l, nil
}

// FileStats reports the write and rotation failures of one channel's file. It
// returns an error when that channel writes no file.
//
// This is the answer to "slog ignores handler errors": a full disk shows up here
// (and through Config.OnError) instead of disappearing.
func (l *Logger) FileStats(channel Channel) (FileStats, error) {
	w, ok := l.files[channel]
	if !ok {
		return FileStats{}, fmt.Errorf("glogs: channel %q has no file", channel)
	}
	return w.stats(), nil
}

// AsyncStats reports drop and error counters for one channel. It returns an error
// when that channel does not use an async writer.
func (l *Logger) AsyncStats(channel Channel) (AsyncStats, error) {
	w, ok := l.async[channel]
	if !ok {
		return AsyncStats{}, fmt.Errorf("glogs: channel %q is not async", channel)
	}
	return w.Stats(), nil
}

// Root returns the root logger (console).
func (l *Logger) Root() *slog.Logger { return l.root }

// Channel returns the logger for a named channel. Unknown channels fall back
// to the root logger.
func (l *Logger) Channel(name Channel) *slog.Logger {
	if lg, ok := l.channels[name]; ok {
		return lg
	}
	return l.root
}

// Close flushes and closes all file writers.
// Close drains async writers, then closes the files. The drain is bounded: a disk
// that stopped accepting writes must not hang shutdown, and unsent lines are
// reported through AsyncStats instead of disappearing silently.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), asyncDrainTimeout)
	defer cancel()

	var firstErr error
	for _, w := range l.async {
		if err := w.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	l.async = map[Channel]*asyncWriter{}

	for _, c := range l.closers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	l.closers = nil
	return firstErr
}

// Stat builds a `key=value ...` line.
type Stat struct {
	attrs []slog.Attr
}

// Stat returns a new Stat builder.
func (l *Logger) Stat() *Stat { return &Stat{} }

// Add appends a key/value pair.
func (s *Stat) Add(key string, value any) *Stat {
	s.attrs = append(s.attrs, slog.Any(key, value))
	return s
}

// Log writes the stat line to the logger.
func (s *Stat) Log(logger *slog.Logger) {
	if len(s.attrs) == 0 {
		return
	}
	var b strings.Builder
	for _, a := range s.attrs {
		b.WriteString("`")
		b.WriteString(a.Key)
		b.WriteString("=")
		b.WriteString(fmt.Sprint(a.Value.Any()))
	}
	logger.Info(b.String())
}

// WithFields returns a context carrying extra structured fields that handlers
// append to every record. This is the Go replacement for LogContext/MDC.
func WithFields(ctx context.Context, attrs ...slog.Attr) context.Context {
	if len(attrs) == 0 {
		return ctx
	}
	existing, _ := ctx.Value(ctxKey{}).([]slog.Attr)
	merged := make([]slog.Attr, 0, len(existing)+len(attrs))
	merged = append(merged, existing...)
	merged = append(merged, attrs...)
	return context.WithValue(ctx, ctxKey{}, merged)
}

type ctxKey struct{}

func contextAttrs(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	attrs, _ := ctx.Value(ctxKey{}).([]slog.Attr)
	return attrs
}

func consoleColor(mode ColorMode) bool {
	switch mode {
	case ColorOn:
		return true
	case ColorOff:
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
