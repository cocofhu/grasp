package services

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// MaxAgentNameRunes is the write-path identity length limit (Unicode code points).
const MaxAgentNameRunes = 64

// ErrInvalidAgentName is returned when a Create/Rename/import-target name fails
// the strict write-identity rules (NFC, charset, length, path-safe).
var ErrInvalidAgentName = errors.New("invalid agent name")

// Write identity: Unicode letters/digits plus ASCII _/- only (no '.').
var agentNameWritePattern = regexp.MustCompile(`^[\p{L}\p{N}_-]+$`)

// fullwidthPunctRe rejects common fullwidth punctuation that must not become identity keys.
var fullwidthPunctRe = regexp.MustCompile(`[－＿．／＼、，。！？：；（）【】]`)

// SuggestAgentRename returns the first free `{name}_vN` (N starting at 2) that
// is not in existing and passes NormalizeAndValidateAgentName.
func SuggestAgentRename(name string, existing map[string]struct{}) string {
	base, err := NormalizeAndValidateAgentName(name)
	if err != nil {
		base = strings.TrimSpace(name)
	}
	n := 2
	for {
		candidate := fmt.Sprintf("%s_v%d", base, n)
		if _, taken := existing[candidate]; !taken {
			if normalized, err := NormalizeAndValidateAgentName(candidate); err == nil {
				return normalized
			}
		}
		n++
		if n > 10000 {
			return candidate
		}
	}
}

// NormalizeAndValidateAgentName NFC-normalizes and validates a write-path Agent name
// (Create / Rename target / import conflict rename). Returns the normalized name.
func NormalizeAndValidateAgentName(raw string) (string, error) {
	name := norm.NFC.String(strings.TrimSpace(raw))
	if name == "" {
		return "", fmt.Errorf("%w: name is required", ErrInvalidAgentName)
	}
	if utf8.RuneCountInString(name) > MaxAgentNameRunes {
		return "", fmt.Errorf("%w: exceeds %d characters", ErrInvalidAgentName, MaxAgentNameRunes)
	}
	if name == "." || name == ".." {
		return "", fmt.Errorf("%w: path meta names are not allowed", ErrInvalidAgentName)
	}
	for _, r := range name {
		if unicode.IsSpace(r) {
			return "", fmt.Errorf("%w: whitespace is not allowed", ErrInvalidAgentName)
		}
		switch r {
		case '.', '/', '\\':
			return "", fmt.Errorf("%w: path separators and '.' are not allowed", ErrInvalidAgentName)
		}
	}
	if fullwidthPunctRe.MatchString(name) {
		return "", fmt.Errorf("%w: fullwidth punctuation is not allowed", ErrInvalidAgentName)
	}
	if !agentNameWritePattern.MatchString(name) {
		return "", fmt.Errorf("%w: only Unicode letters/digits plus _ and - (max %d)", ErrInvalidAgentName, MaxAgentNameRunes)
	}
	return name, nil
}

// sanitizeAgentPath returns a single-segment path-safe agent directory name, or "".
// Accepts the write-identity charset (Unicode L/N + `_-`).
func sanitizeAgentPath(name string) string {
	name = strings.Trim(name, "/\\ ")
	base := filepath.Base(name)
	if !agentNameWritePattern.MatchString(base) {
		return ""
	}
	return base
}
