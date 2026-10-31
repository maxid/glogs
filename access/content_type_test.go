package access

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/maxid/glogs"
)

func TestLoggableContentTypes(t *testing.T) {
	cases := map[string]bool{
		"application/json":                  true,
		"application/json; charset=utf-8":   true,
		"application/xml":                   true,
		"text/plain":                        true,
		"text/html; charset=utf-8":          true,
		"application/x-www-form-urlencoded": true,
		"":                                  false, // no declared type: do not guess
		"image/png":                         false,
		"application/octet-stream":          false,
		"multipart/form-data; boundary=x":   false,
	}
	for contentType, want := range cases {
		if got := loggable(contentType, nil); got != want {
			t.Errorf("loggable(%q) = %v, want %v", contentType, got, want)
		}
	}

	// An extra allow entry can add a type.
	if !loggable("application/vnd.api+json", []string{"application/vnd.api+json"}) {
		t.Error("extra allow list ignored")
	}
}

// A binary response must not be copied into the log, even with capture enabled.
func TestBinaryBodiesAreNotLogged(t *testing.T) {
	var captured bytes.Buffer
	logger, err := glogs.New(glogs.Config{
		AppName: "ct",
		Dir:     t.TempDir(),
		Root:    glogs.ConsoleConfig{Enabled: false},
		Channels: map[glogs.Channel]glogs.ChannelConfig{
			glogs.ChannelAccess: {Enabled: true, Path: t.TempDir() + "/access.log", Console: false},
		},
	})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	defer logger.Close()

	_ = captured
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', 0x00})
	})
	middleware := Middleware(logger, WithBodyLimit(128))(handler)

	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(`{"password":"s3cret"}`))
	req.Header.Set("Content-Type", "application/json")
	middleware.ServeHTTP(httptest.NewRecorder(), req)
}

// With a redactor and a JSON content type, a credential never reaches the log.
func TestAccessBodyIsRedacted(t *testing.T) {
	dir := t.TempDir()
	logger, err := glogs.New(glogs.Config{
		AppName: "redact",
		Dir:     dir,
		// JSON so the assertions can parse the line; the file handler follows the
		// root format.
		Root: glogs.ConsoleConfig{Enabled: false, Format: glogs.FormatJSON},
		Channels: map[glogs.Channel]glogs.ChannelConfig{
			glogs.ChannelAccess: {Enabled: true, Path: dir + "/access.log", Console: false},
		},
	})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}

	redactor := glogs.NewMaskRedactor("")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"token":"resp-token"}`))
	})
	middleware := Middleware(logger,
		WithBodyLimit(256),
		WithBodyRedactor(func(body string) string { return redactor.RedactBody([]byte(body), false) }),
	)(handler)

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"alice","password":"req-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	middleware.ServeHTTP(httptest.NewRecorder(), req)

	if err := logger.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	content := readAll(t, dir+"/access.log")
	if strings.Contains(content, "req-secret") || strings.Contains(content, "resp-token") {
		t.Fatalf("credential reached the access log:\n%s", content)
	}
	if !strings.Contains(content, "alice") {
		t.Fatalf("non-sensitive body content was lost:\n%s", content)
	}

	// The body is still JSON, so a log consumer keeps working.
	for _, line := range strings.Split(strings.TrimSpace(content), "\n") {
		if line == "" {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("log line is not JSON: %v (%s)", err, line)
		}
	}
}

func readAll(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}
