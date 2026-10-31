package access

// BodyRedactor masks sensitive fields in a captured body. The middleware keeps it
// optional so the package does not depend on a particular redaction policy.
type BodyRedactor struct {
	Mask func(body string) string
}

func (r *BodyRedactor) body(raw string) string {
	if r == nil || r.Mask == nil || raw == "" {
		return raw
	}
	return r.Mask(raw)
}

// WithBodyRedactor masks captured request and response bodies.
func WithBodyRedactor(mask func(body string) string) Option {
	return func(o *options) {
		if mask != nil {
			o.redactor = &BodyRedactor{Mask: mask}
		}
	}
}

// responseBody returns the captured response body, respecting the content-type
// allow list and the redactor.
func (o *options) responseBody(rec *responseRecorder) string {
	if rec.body.Len() == 0 || !responseLoggable(rec, o.loggable) {
		return ""
	}
	return o.redactor.body(rec.body.String())
}
