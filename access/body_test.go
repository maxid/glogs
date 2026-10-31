package access

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxid/glogs"
)

// The decisive property: logging must never change what the handler receives.
// The previous implementation replaced the body with the truncated prefix.
func TestBodyCaptureKeepsTheFullStream(t *testing.T) {
	const limit = 8
	payload := strings.Repeat("abcdefghij", 100) // 1000 bytes, well over the limit

	var received int
	var head string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		received = len(body)
		head = string(body[:min(len(body), 10)])
	})

	logger, _ := glogs.New(glogs.Config{})
	middleware := Middleware(logger, WithBodyLimit(limit))(handler)

	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(payload))
	middleware.ServeHTTP(httptest.NewRecorder(), req)

	if received != len(payload) {
		t.Fatalf("handler received %d of %d bytes: the log limited the request", received, len(payload))
	}
	if !strings.HasPrefix(head, payload[:10]) {
		t.Fatalf("handler saw %q, want the original prefix", head)
	}
}

// A short body is captured whole and reported without a truncation marker.
func TestBodyCaptureShortBody(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("short"))
	if got := captureRequest(r, 64); got != "short" {
		t.Fatalf("captured = %q, want short", got)
	}
	rest, _ := io.ReadAll(r.Body)
	if string(rest) != "short" {
		t.Fatalf("handler stream = %q, want short", rest)
	}
}

// Exactly at the limit with more data behind it: the handler must still see
// everything.
func TestBodyCaptureAtLimitBoundary(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 16)
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(payload))

	got := captureRequest(r, 8)
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Fatalf("captured = %q, want a truncation marker", got)
	}
	rest, _ := io.ReadAll(r.Body)
	if len(rest) != len(payload) {
		t.Fatalf("handler stream = %d bytes, want %d", len(rest), len(payload))
	}
}

func TestClientIPIgnoresUntrustedForwardingHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.Header.Set("X-Real-IP", "5.6.7.8")

	if got := clientIP(r, nil); got != "203.0.113.9" {
		t.Fatalf("client ip = %q, want the peer address", got)
	}
}

func TestClientIPTrustsConfiguredProxies(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.5:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.5")

	trusted := parseTrustedProxies([]string{"10.0.0.0/8"})
	if got := clientIP(r, trusted); got != "203.0.113.9" {
		t.Fatalf("client ip = %q, want the leftmost untrusted hop", got)
	}
}

func TestClientIPFallsBackToXRealIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.5:1234"
	r.Header.Set("X-Real-IP", "203.0.113.9")

	trusted := parseTrustedProxies([]string{"10.0.0.5"})
	if got := clientIP(r, trusted); got != "203.0.113.9" {
		t.Fatalf("client ip = %q, want X-Real-IP", got)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
