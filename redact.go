package glogs

import (
	"encoding/json"
	"strings"
)

// DefaultMask replaces a redacted value.
const DefaultMask = "***"

// DefaultSensitiveKeys is the deny list applied by key name, case-insensitively.
// It is intentionally coarse: a false positive costs a masked debug field, a false
// negative writes a credential to disk.
var DefaultSensitiveKeys = []string{
	"password", "passwd", "pwd", "secret", "token", "accesstoken", "refreshtoken",
	"verifytoken", "authorization", "cookie", "set-cookie", "credential",
	"privatekey", "apikey", "api-key", "sessionid", "session", "salt",
	"idcard", "idnumber", "bankcard", "cvv",
	// Chinese field names that appear in the platform's DTOs.
	"密码", "令牌", "密钥",
}

// Redactor decides what must not reach a log file, and what a masked value looks
// like. The default implementation covers attribute keys and JSON bodies; a
// deployment can replace it.
type Redactor interface {
	// RedactValue masks a value whose key matched (or returns it unchanged).
	RedactValue(key string, value any) any
	// Sensitive reports whether an attribute key is sensitive.
	Sensitive(key string) bool
	// RedactBody masks the sensitive fields of a JSON request/response body and
	// returns the rewritten body. Bodies that are not JSON are returned as is
	// unless maskAll is set.
	RedactBody(body []byte, maskAll bool) string
}

// MaskRedactor is the default Redactor: substring match on key names plus a
// JSON-field walker for bodies.
type MaskRedactor struct {
	// Keys extends DefaultSensitiveKeys.
	Keys []string
	// Mask is the replacement text; DefaultMask when empty.
	Mask string
	// MaxBodyBytes bounds how much of a body is walked or returned.
	MaxBodyBytes int
}

// NewMaskRedactor builds a redactor with the default deny list plus extra keys.
func NewMaskRedactor(mask string, extraKeys ...string) *MaskRedactor {
	if mask == "" {
		mask = DefaultMask
	}
	return &MaskRedactor{Keys: extraKeys, Mask: mask, MaxBodyBytes: 4096}
}

// Sensitive implements Redactor.
func (r *MaskRedactor) Sensitive(key string) bool {
	lower := strings.ToLower(key)
	for _, candidate := range DefaultSensitiveKeys {
		if strings.Contains(lower, candidate) {
			return true
		}
	}
	for _, candidate := range r.Keys {
		if candidate == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

// RedactValue implements Redactor.
func (r *MaskRedactor) RedactValue(key string, value any) any {
	if !r.Sensitive(key) {
		return value
	}
	return r.mask()
}

func (r *MaskRedactor) mask() string {
	if r.Mask == "" {
		return DefaultMask
	}
	return r.Mask
}

// RedactBody implements Redactor.
//
// A body that cannot be parsed as JSON is masked entirely when maskAll is set (a
// non-JSON body might be anything, so keeping it would defeat the purpose), and
// returned untouched otherwise. The size bound keeps a large upload from being
// copied into the log path.
func (r *MaskRedactor) RedactBody(body []byte, maskAll bool) string {
	limit := r.MaxBodyBytes
	if limit <= 0 {
		limit = 4096
	}

	if len(body) > limit {
		return string(body[:limit]) + "...(truncated)"
	}
	if len(body) == 0 {
		return ""
	}

	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		if maskAll {
			return r.mask()
		}
		return string(body)
	}

	masked := r.redactJSON(decoded)
	raw, err := json.Marshal(masked)
	if err != nil {
		return r.mask()
	}
	return string(raw)
}

// redactJSON walks the decoded body and masks sensitive keys at any depth.
func (r *MaskRedactor) redactJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, field := range typed {
			if r.Sensitive(key) {
				out[key] = r.mask()
				continue
			}
			out[key] = r.redactJSON(field)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, element := range typed {
			out[i] = r.redactJSON(element)
		}
		return out
	default:
		return value
	}
}
