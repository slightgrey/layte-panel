package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// secretKeyHints: any JSON key containing one of these (case-insensitive) is redacted.
var secretKeyHints = []string{"password", "passwd", "secret", "token", "key", "otp"}

// RedactJSON returns params as JSON with secret-looking values replaced by "[redacted]".
func RedactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return `"[unparseable]"`
	}
	b, _ := json.Marshal(redact(v))
	return string(b)
}

func redact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if isSecretKey(k) {
				t[k] = "[redacted]"
			} else {
				t[k] = redact(val)
			}
		}
		return t
	case []any:
		for i := range t {
			t[i] = redact(t[i])
		}
		return t
	case string:
		// Big payloads (file chunks) don't belong in the audit log.
		if len(t) > maxAuditString {
			return fmt.Sprintf("[%d bytes]", len(t))
		}
		return t
	default:
		return v
	}
}

const maxAuditString = 512

func isSecretKey(k string) bool {
	k = strings.ToLower(k)
	for _, h := range secretKeyHints {
		if strings.Contains(k, h) {
			return true
		}
	}
	return false
}
