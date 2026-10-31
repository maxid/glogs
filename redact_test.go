package glogs

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestSensitiveKeys(t *testing.T) {
	r := NewMaskRedactor("")

	cases := map[string]bool{
		"password":      true,
		"oldPassword":   true,
		"accessToken":   true,
		"Authorization": true,
		"cookie":        true,
		"idCard":        true,
		"密码":            true,
		"requestId":     false,
		"uri":           false,
		"status":        false,
	}
	for key, want := range cases {
		if got := r.Sensitive(key); got != want {
			t.Errorf("Sensitive(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestRedactBody(t *testing.T) {
	r := NewMaskRedactor("")
	body := []byte(`{"username":"alice","password":"s3cret","nested":{"accessToken":"tk","safe":"ok"},"list":[{"token":"x"}]}`)

	got := r.RedactBody(body, false)

	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("output is not JSON: %v (%s)", err, got)
	}
	if decoded["password"] != DefaultMask {
		t.Errorf("password = %v, want masked", decoded["password"])
	}
	if decoded["username"] != "alice" {
		t.Errorf("username = %v, want untouched", decoded["username"])
	}
	nested, _ := decoded["nested"].(map[string]any)
	if nested["accessToken"] != DefaultMask || nested["safe"] != "ok" {
		t.Errorf("nested = %v", nested)
	}
	list, _ := decoded["list"].([]any)
	first, _ := list[0].(map[string]any)
	if first["token"] != DefaultMask {
		t.Errorf("list entry = %v, want masked", first)
	}
}

// A body that is not JSON cannot be inspected field by field, so the caller decides:
// mask it whole or keep it.
func TestRedactNonJSONBody(t *testing.T) {
	r := NewMaskRedactor("")

	if got := r.RedactBody([]byte("password=hunter2"), true); got != DefaultMask {
		t.Fatalf("maskAll = %q, want the mask", got)
	}
	if got := r.RedactBody([]byte("plain text"), false); got != "plain text" {
		t.Fatalf("unmasked = %q, want the original", got)
	}
}

func TestRedactBodyTruncates(t *testing.T) {
	r := NewMaskRedactor("")
	r.MaxBodyBytes = 8

	got := r.RedactBody([]byte(`{"password":"a-very-long-value"}`), false)
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Fatalf("oversized body = %q, want a truncation marker", got)
	}
}

func TestExtraKeys(t *testing.T) {
	r := NewMaskRedactor("", "deviceSecret")
	if !r.Sensitive("deviceSecret") {
		t.Fatal("extra key not honoured")
	}
}

// The whole point: a credential must never reach the file handler.
func TestHandlerMasksSensitiveAttributes(t *testing.T) {
	var buf bytes.Buffer
	handler := newHandler(&buf, FormatJSON, false, slog.LevelInfo, "app", NewMaskRedactor(""))
	logger := slog.New(handler)

	logger.Info("login",
		"username", "alice",
		"password", "s3cret",
		slog.Group("auth", "accessToken", "tk-123", "method", "password"),
	)

	out := buf.String()
	if strings.Contains(out, "s3cret") || strings.Contains(out, "tk-123") {
		t.Fatalf("credential reached the log: %s", out)
	}
	if !strings.Contains(out, "alice") {
		t.Fatalf("non-sensitive field was lost: %s", out)
	}

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if decoded["password"] != DefaultMask {
		t.Errorf("password = %v", decoded["password"])
	}
	auth, _ := decoded["auth"].(map[string]any)
	if auth["accessToken"] != DefaultMask || auth["method"] != "password" {
		t.Errorf("group = %v", auth)
	}
}

func TestTextHandlerMasksSensitiveAttributes(t *testing.T) {
	var buf bytes.Buffer
	handler := newHandler(&buf, FormatText, false, slog.LevelInfo, "app", NewMaskRedactor(""))
	logger := slog.New(handler)

	logger.Info("login", "password", "s3cret", "username", "alice")

	out := buf.String()
	if strings.Contains(out, "s3cret") {
		t.Fatalf("credential reached the text log: %s", out)
	}
	if !strings.Contains(out, "alice") {
		t.Fatalf("non-sensitive field was lost: %s", out)
	}
}

// Without a redactor nothing changes: the default must not silently alter output
// for deployments that already rely on the current format.
func TestNoRedactorLeavesValues(t *testing.T) {
	var buf bytes.Buffer
	handler := newHandler(&buf, FormatText, false, slog.LevelInfo, "app", nil)
	slog.New(handler).Info("login", "password", "s3cret")

	if !strings.Contains(buf.String(), "s3cret") {
		t.Fatalf("value was masked without a redactor: %s", buf.String())
	}
}
