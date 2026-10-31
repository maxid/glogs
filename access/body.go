package access

import (
	"bytes"
	"io"
	"net/http"
)

// capture wraps the request body so the handler still receives every byte while
// the log keeps at most limit bytes.
//
// The earlier version read `limit+1` bytes and replaced the body with just those
// bytes, so any request larger than the limit reached the handler truncated. Here
// the captured prefix is prepended to the untouched remainder: the handler sees
// the full stream, the log sees a bounded excerpt.
func captureRequest(r *http.Request, limit int) string {
	if limit <= 0 || r.Body == nil {
		return ""
	}

	prefix, err := io.ReadAll(io.LimitReader(r.Body, int64(limit)))
	if err != nil {
		// Reading failed: leave the body alone rather than silently dropping the
		// rest, and log nothing.
		return ""
	}

	// Anything left unread stays available to the handler.
	rest := r.Body
	r.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: io.MultiReader(bytes.NewReader(prefix), rest),
		Closer: rest,
	}

	if len(prefix) < limit {
		return string(prefix)
	}
	// Exactly `limit` bytes were captured; there may or may not be more. Probe one
	// byte without consuming it from the handler's stream.
	var probe [1]byte
	n, _ := rest.Read(probe[:])
	if n > 0 {
		r.Body = struct {
			io.Reader
			io.Closer
		}{
			Reader: io.MultiReader(bytes.NewReader(prefix), bytes.NewReader(probe[:n]), rest),
			Closer: rest,
		}
		return string(prefix) + "...(truncated)"
	}
	return string(prefix)
}
