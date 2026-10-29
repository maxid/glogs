package glogs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	ansiReset  = "\x1b[0m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
	ansiFaint  = "\x1b[2m"
)

// newHandler builds a text or JSON handler for a channel.
func newHandler(w io.Writer, format Format, color bool, level slog.Level, channel string) slog.Handler {
	if format == FormatJSON {
		return &jsonHandler{inner: slog.NewJSONHandler(w, &slog.HandlerOptions{
			Level: level,
			ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
				if a.Key == slog.TimeKey {
					return slog.String("ts", a.Value.Time().Format("2006-01-02T15:04:05.000Z07:00"))
				}
				if a.Key == slog.MessageKey {
					return slog.String("message", a.Value.String())
				}
				return a
			},
		}), channel: channel}
	}
	return &textHandler{mu: &sync.Mutex{}, out: w, color: color, level: level, channel: channel}
}

// multiHandler fans a record out to several handlers (file + console).
type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range m.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		out[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: out}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	out := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		out[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: out}
}

type textHandler struct {
	mu      *sync.Mutex
	out     io.Writer
	color   bool
	level   slog.Level
	channel string
	attrs   []slog.Attr
	groups  []string
}

func (h *textHandler) Enabled(_ context.Context, lv slog.Level) bool {
	return lv >= h.level
}

func (h *textHandler) Handle(ctx context.Context, r slog.Record) error {
	var b strings.Builder
	ts := r.Time.Format("2006-01-02 15:04:05.000")
	lv := r.Level.String()
	if h.color {
		b.WriteString(ansiFaint)
		b.WriteString(ts)
		b.WriteString(ansiReset)
		b.WriteByte(' ')
		b.WriteString(levelColor(r.Level))
		b.WriteString(fmt.Sprintf("%-5s", lv))
		b.WriteString(ansiReset)
		b.WriteByte(' ')
		b.WriteString(ansiCyan)
		b.WriteString("[" + h.channel + "]")
		b.WriteString(ansiReset)
	} else {
		b.WriteString(ts)
		b.WriteByte(' ')
		b.WriteString(fmt.Sprintf("%-5s", lv))
		b.WriteString(" [" + h.channel + "]")
	}
	b.WriteByte(' ')
	b.WriteString(r.Message)

	for _, a := range h.attrs {
		writeAttr(&b, h.groups, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		writeAttr(&b, h.groups, a)
		return true
	})
	for _, a := range contextAttrs(ctx) {
		writeAttr(&b, h.groups, a)
	}
	b.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.out, b.String())
	return err
}

func (h *textHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cp := *h
	cp.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &cp
}

func (h *textHandler) WithGroup(name string) slog.Handler {
	cp := *h
	cp.groups = append(append([]string(nil), h.groups...), name)
	return &cp
}

type jsonHandler struct {
	inner   slog.Handler
	channel string
}

func (h *jsonHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *jsonHandler) Handle(ctx context.Context, r slog.Record) error {
	r2 := r.Clone()
	r2.AddAttrs(slog.String("channel", h.channel))
	for _, a := range contextAttrs(ctx) {
		r2.AddAttrs(a)
	}
	return h.inner.Handle(ctx, r2)
}

func (h *jsonHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &jsonHandler{inner: h.inner.WithAttrs(attrs), channel: h.channel}
}

func (h *jsonHandler) WithGroup(name string) slog.Handler {
	return &jsonHandler{inner: h.inner.WithGroup(name), channel: h.channel}
}

func writeAttr(b *strings.Builder, groups []string, a slog.Attr) {
	if a.Equal(slog.Attr{}) {
		return
	}
	key := a.Key
	if len(groups) > 0 {
		key = strings.Join(groups, ".") + "." + key
	}
	b.WriteByte(' ')
	b.WriteString(key)
	b.WriteByte('=')
	b.WriteString(formatValue(a.Value))
}

func formatValue(v slog.Value) string {
	v = v.Resolve()
	switch v.Kind() {
	case slog.KindString:
		s := v.String()
		if needsQuoting(s) {
			return strconv.Quote(s)
		}
		return s
	case slog.KindTime:
		return v.Time().Format("2006-01-02T15:04:05.000Z07:00")
	case slog.KindGroup:
		b, _ := json.Marshal(groupMap(v.Group()))
		return string(b)
	default:
		return fmt.Sprint(v.Any())
	}
}

func groupMap(attrs []slog.Attr) map[string]any {
	m := make(map[string]any, len(attrs))
	for _, a := range attrs {
		if a.Value.Kind() == slog.KindGroup {
			m[a.Key] = groupMap(a.Value.Group())
		} else {
			m[a.Key] = a.Value.Any()
		}
	}
	return m
}

func needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if unicode.IsSpace(r) || r == '"' || r == '=' || r < utf8.RuneSelf && !unicode.IsPrint(r) {
			return true
		}
	}
	return false
}

func levelColor(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return ansiRed
	case l >= slog.LevelWarn:
		return ansiYellow
	case l >= slog.LevelInfo:
		return ansiGreen
	default:
		return ansiCyan
	}
}
