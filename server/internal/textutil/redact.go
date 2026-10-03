package textutil

import "regexp"

// SecretMask replaces a redacted secret value.
const SecretMask = "****"

// secretValueREs match common embedded secret shapes inside free text. Patterns
// with two groups keep the first (the "key=" prefix) and mask the second.
var secretValueREs = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(sk-[A-Za-z0-9_\-]{16,})\b`),
	regexp.MustCompile(`(?i)\b(ghp_[A-Za-z0-9]{20,})\b`),
	regexp.MustCompile(`(?i)\b(github_pat_[A-Za-z0-9_]{20,})\b`),
	regexp.MustCompile(`(?i)\b(xox[baprs]-[A-Za-z0-9\-]{10,})\b`),
	regexp.MustCompile(`(?i)\b(Bearer\s+[A-Za-z0-9\-_\.=]{16,})\b`),
	regexp.MustCompile(`(?i)\b((?:api[_-]?key|password|passwd|secret|token)\s*[:=]\s*)([^\s"'\\]{6,})`),
}

// RedactSecrets masks token / key / password shapes embedded in s.
func RedactSecrets(s string) string {
	out := s
	for _, re := range secretValueREs {
		if re.NumSubexp() >= 2 {
			out = re.ReplaceAllString(out, "${1}"+SecretMask)
		} else {
			out = re.ReplaceAllString(out, SecretMask)
		}
	}
	return out
}
