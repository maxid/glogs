package access

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxid/glogs"
)

func TestMiddlewareLogsAccess(t *testing.T) {
	dir := t.TempDir()
	cfg := glogs.DefaultConfig("test")
	cfg.Dir = dir
	for name, cc := range cfg.Channels {
		cc.Path = filepath.Join(dir, "common", string(name), string(name)+".log")
		cfg.Channels[name] = cc
	}
	l, err := glogs.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	h := Middleware(l, WithBodyLimit(64))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/devices?x=1", strings.NewReader("body"))
	req.Header.Set("X-Trace-Id", "trace-1")
	h.ServeHTTP(httptest.NewRecorder(), req)

	b, _ := os.ReadFile(cfg.Channels[glogs.ChannelAccess].Path)
	out := string(b)
	for _, want := range []string{"uri=\"/api/devices?x=1\"", "method=POST", "status=201", "trace_id=trace-1", "resp_body=ok"} {
		if !strings.Contains(out, want) {
			t.Fatalf("access log missing %q: %s", want, out)
		}
	}
}
