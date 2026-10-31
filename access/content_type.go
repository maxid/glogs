package access

import (
	"mime"
	"net/http"
	"strings"
)

// defaultLoggableTypes are the content types whose bodies are safe to copy into a
// log line. Everything else — images, streams, protobufs, file downloads — is
// skipped: a binary body would either be garbage in the log or, worse, a payload
// nobody meant to persist.
var defaultLoggableTypes = []string{
	"application/json",
	"application/xml",
	"application/x-www-form-urlencoded",
	"text/",
}

// loggable reports whether a content type may be captured.
//
// An empty content type is treated as not loggable: without a declared type there
// is no way to know what the body holds, and guessing is how credentials end up in
// a log file.
func loggable(contentType string, extra []string) bool {
	if contentType == "" {
		return false
	}
	mediatype, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediatype = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	mediatype = strings.ToLower(mediatype)

	// A charset the caller did not ask for does not change the decision; the
	// parameters are parsed only so a malformed header does not slip through.
	_ = params

	for _, allowed := range extra {
		if allowed == "" {
			continue
		}
		allowed = strings.ToLower(allowed)
		if mediatype == allowed || (strings.HasSuffix(allowed, "/") && strings.HasPrefix(mediatype, allowed)) {
			return true
		}
	}
	for _, allowed := range defaultLoggableTypes {
		if strings.HasSuffix(allowed, "/") {
			if strings.HasPrefix(mediatype, allowed) {
				return true
			}
			continue
		}
		if mediatype == allowed {
			return true
		}
	}
	return false
}

// WithLoggableContentTypes replaces the default content-type allow list.
func WithLoggableContentTypes(types ...string) Option {
	return func(o *options) { o.loggable = types }
}

// requestLoggable decides from the request's declared type.
func requestLoggable(r *http.Request, extra []string) bool {
	return loggable(r.Header.Get("Content-Type"), extra)
}

// responseLoggable decides from what the handler wrote.
func responseLoggable(rec *responseRecorder, extra []string) bool {
	if contentType := rec.Header().Get("Content-Type"); contentType != "" {
		return loggable(contentType, extra)
	}
	// The handler may have written without setting a header; Go sniffs it.
	return loggable(http.DetectContentType(rec.body.Bytes()), extra)
}
