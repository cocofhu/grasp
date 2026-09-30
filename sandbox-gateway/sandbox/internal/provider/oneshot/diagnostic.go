package oneshot

import (
	"regexp"
	"strings"
)

var (
	diagnosticAuth       = regexp.MustCompile(`(?i)\b((?:proxy-)?authorization["']?\s*[:=]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\r\n,;}]+)`)
	diagnosticCredential = regexp.MustCompile(`(?i)\b([a-z0-9_-]*(?:api[_-]?key|token|password|passwd|secret|credential)["']?\s*[:=]\s*)(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s,;&"'}]+)`)
	diagnosticBearer     = regexp.MustCompile(`(?i)\b(?:Bearer|Basic)\s+[a-z0-9._~+/=-]+`)
	diagnosticUserInfo   = regexp.MustCompile(`(?i)(https?://)[^/\s@]+@`)
)

// emitError sends CLI diagnostics before the turn boundary. stderr may include
// HTTP debug output: preserve the connection failure while hiding credentials.
func (e *engine) emitError(err error) {
	e.emit(map[string]any{"op": "raw", "type": "error_text", "text": redactDiagnostic(err.Error(), e.env)})
}

// The bridge also logs and broadcasts Prompt's returned error. Keep that path
// redacted too, while preserving cancellation and native exit error matching.
type safeDiagnosticError struct {
	cause error
	text  string
}

func (e *safeDiagnosticError) Error() string { return e.text }
func (e *safeDiagnosticError) Unwrap() error { return e.cause }

func (e *engine) safeError(err error) error {
	text := redactDiagnostic(err.Error(), e.env)
	if text == err.Error() {
		return err
	}
	return &safeDiagnosticError{cause: err, text: text}
}

func redactDiagnostic(text string, env []string) string {
	// CLI auth normalization has already run, so native credential variables
	// are available even when the caller used an agent-independent name.
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || len(value) < 4 {
			continue
		}
		name = strings.ToLower(name)
		for _, hint := range []string{"api_key", "apikey", "token", "secret", "password", "passwd", "credential", "authorization"} {
			if strings.Contains(name, hint) {
				text = strings.ReplaceAll(text, value, "[redacted]")
				break
			}
		}
	}
	text = diagnosticAuth.ReplaceAllString(text, "${1}[redacted]")
	text = diagnosticCredential.ReplaceAllString(text, "${1}[redacted]")
	text = diagnosticBearer.ReplaceAllString(text, "[redacted]")
	return diagnosticUserInfo.ReplaceAllString(text, "${1}[redacted]@")
}
