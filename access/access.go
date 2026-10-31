// Package access provides HTTP access logging middleware backed by glogs.
package access

import (
	"bytes"
	"net/http"
	"time"

	"github.com/maxid/glogs"
)

// Option configures the middleware.
type Option func(*options)

type options struct {
	bodyLimit int
	skip      func(*http.Request) bool
	trusted   *trustedProxies
}

// WithTrustedProxies restricts which peers may set X-Forwarded-For / X-Real-IP.
// Without it those headers are ignored, because a directly reachable client can
// forge them and the logged client_ip would be attacker controlled.
func WithTrustedProxies(cidrs ...string) Option {
	return func(o *options) {
		o.trusted = parseTrustedProxies(cidrs)
	}
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
				"client_ip", clientIP(r, cfg.trusted),
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
