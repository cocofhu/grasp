package provider

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

// RedactDiagnostic hides credentials in CLI diagnostics (stderr may include
// HTTP debug output) while keeping the failure readable. env is the agent's
// environment after auth normalization, so native credential variables are
// present even when the caller used an agent-independent name.
func RedactDiagnostic(text string, env []string) string {
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
