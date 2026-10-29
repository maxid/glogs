// Package access provides HTTP access logging middleware backed by glogs.
package access

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/maxid/glogs"
)

// Option configures the middleware.
type Option func(*options)

type options struct {
	bodyLimit int
	skip      func(*http.Request) bool
}

// WithBodyLimit enables request/response body capture up to n bytes.
func WithBodyLimit(n int) Option { return func(o *options) { o.bodyLimit = n } }

// WithSkip skips logging for matching requests (e.g. health checks).
func WithSkip(fn func(*http.Request) bool) Option { return func(o *options) { o.skip = fn } }

// Middleware returns an access-logging middleware writing to the "access" channel.
func Middleware(logger *glogs.Logger, opts ...Option) func(http.Handler) http.Handler {
	cfg := options{bodyLimit: 0}
	for _, opt := range opts {
		opt(&cfg)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.skip != nil && cfg.skip(r) {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			reqBody := captureRequest(r, cfg.bodyLimit)
			rec := &responseRecorder{ResponseWriter: w, limit: cfg.bodyLimit}
			next.ServeHTTP(rec, r)
			end := time.Now()

			attrs := []any{
				"uri", r.URL.RequestURI(),
				"method", r.Method,
				"status", rec.status,
				"cost_ms", end.Sub(start).Milliseconds(),
				"start_tm", start.Format("2006-01-02 15:04:05.000"),
				"end_tm", end.Format("2006-01-02 15:04:05.000"),
				"client_ip", clientIP(r),
				"user_agent", r.UserAgent(),
				"req_body", reqBody,
				"resp_body", rec.body.String(),
			}
			if v := r.Header.Get("X-Trace-Id"); v != "" {
				attrs = append(attrs, "trace_id", v)
			}
			logger.Channel(glogs.ChannelAccess).InfoContext(r.Context(), "access", attrs...)
		})
	}
}

func captureRequest(r *http.Request, limit int) string {
	if limit <= 0 || r.Body == nil {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(limit)+1))
	if err != nil {
		return ""
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) > limit {
		return string(body[:limit]) + "...(truncated)"
	}
	return string(body)
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	limit  int
	body   bytes.Buffer
}

func (r *responseRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.limit > 0 && r.body.Len() < r.limit {
		remain := r.limit - r.body.Len()
		if remain > len(p) {
			remain = len(p)
		}
		r.body.Write(p[:remain])
	}
	return r.ResponseWriter.Write(p)
}

func (r *responseRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
