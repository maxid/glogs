// Package bridge captures the output of loggers that are not slog-based.
//
// A dependency usually offers no way to replace its logger: it writes to an
// io.Writer, or to the standard library's log package. The bridge turns those
// lines into glogs records so one file, one format and one redaction policy cover
// the whole process.
//
// What it cannot recover is structure. A line-based bridge knows the text and (if
// asked) a level token, nothing else — a library that logs structured fields
// through its own logger will have them flattened into the message. For libraries
// that do expose a structured seam (slog, zapcore), prefer a native adapter.
package bridge

import (
	"bytes"
	"io"
	"log"
	"log/slog"
	"strings"
	"sync"

	"github.com/maxid/glogs"
)

// Options configure a bridge.
type Options struct {
	// Channel receives the records. ChannelDefault when empty.
	Channel glogs.Channel
	// Source labels the emitting library ("zap", "logrus", …) and is attached to
	// every record so the origin of a line stays visible.
	Source string
	// Level is the level every record is written at, unless SniffLevel is set.
	Level slog.Level
	// SniffLevel reads a leading or bracketed level token from the line
	// (DEBUG/INFO/WARN/ERROR in any case) and uses it when recognised.
	SniffLevel bool
	// MaxLineBytes bounds one captured line; longer lines are truncated. A
	// library that dumps a payload must not fill the log file with it.
	MaxLineBytes int
}

func (o Options) normalized() Options {
	if o.Channel == "" {
		o.Channel = glogs.ChannelDefault
	}
	if o.MaxLineBytes <= 0 {
		o.MaxLineBytes = 8 << 10
	}
	return o
}

// Writer returns an io.Writer that forwards complete lines to the logger.
//
// Most loggers accept one (logrus.SetOutput, zerolog.New, log.New,
// zapcore.AddSync). Writes may split a line, so the writer buffers until it sees a
// newline; Close flushes a trailing partial line.
type Writer struct {
	logger  *glogs.Logger
	opts    Options
	mu      sync.Mutex
	partial []byte
	closed  bool
}

// NewWriter builds a line-forwarding writer.
func NewWriter(logger *glogs.Logger, opts Options) *Writer {
	return &Writer{logger: logger, opts: opts.normalized()}
}

// Write implements io.Writer.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, io.ErrClosedPipe
	}

	chunk := append(w.partial, p...)
	lines := bytes.Split(chunk, []byte("\n"))

	// The last element has no trailing newline yet: keep it for the next write.
	w.partial = append([]byte(nil), lines[len(lines)-1]...)
	for _, line := range lines[:len(lines)-1] {
		w.emit(string(line))
	}
	return len(p), nil
}

// Close flushes a trailing partial line.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}
	w.closed = true
	if len(w.partial) > 0 {
		w.emit(string(w.partial))
		w.partial = nil
	}
	return nil
}

// emit writes one line as a record.
func (w *Writer) emit(line string) {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	if len(line) > w.opts.MaxLineBytes {
		line = line[:w.opts.MaxLineBytes] + "...(truncated)"
	}

	level := w.opts.Level
	if w.opts.SniffLevel {
		if sniffed, ok := sniffLevel(line); ok {
			level = sniffed
		}
	}

	attrs := []any{"msg", line}
	if w.opts.Source != "" {
		attrs = append(attrs, "source", w.opts.Source)
	}
	w.logger.Channel(w.opts.Channel).Log(nil, level, "bridge", attrs...)
}

// sniffLevel recognises a level token at the start of a line or in square
// brackets, which is how zap, logrus and the standard library's log prefix render
// it by default.
func sniffLevel(line string) (slog.Level, bool) {
	candidate := strings.TrimSpace(line)
	if strings.HasPrefix(candidate, "[") {
		if end := strings.Index(candidate, "]"); end > 1 {
			candidate = candidate[1:end]
		}
	}
	field := candidate
	if idx := strings.IndexAny(field, " \t:"); idx > 0 {
		field = field[:idx]
	}

	switch strings.ToUpper(field) {
	case "TRACE", "DEBUG", "DBG":
		return slog.LevelDebug, true
	case "INFO", "INF":
		return slog.LevelInfo, true
	case "WARN", "WARNING", "WRN":
		return slog.LevelWarn, true
	case "ERROR", "ERR", "FATAL", "PANIC", "CRITICAL", "CRIT":
		return slog.LevelError, true
	default:
		return 0, false
	}
}

// Stdlib redirects the standard library logger into the given channel and returns a
// function that restores the previous output and flags. Many third-party packages
// simply call log.Printf, so this single line covers a large share of them.
func Stdlib(logger *glogs.Logger, opts Options) func() {
	writer := NewWriter(logger, opts)

	previousWriter := log.Writer()
	previousFlags := log.Flags()

	log.SetOutput(writer)
	// The bridge adds its own timestamp; the stdlib prefix would duplicate it.
	log.SetFlags(0)

	return func() {
		_ = writer.Close()
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	}
}

// WriterOption adapts a writer for libraries that want a zapcore.WriteSyncer
// shape; AddSync is satisfied by any io.Writer wrapper, so this is a convenience
// alias rather than a type conversion.
func AddSync(writer io.Writer) io.Writer { return writer }
