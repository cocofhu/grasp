package blob

import (
	"net/http"
	"strings"
)

// StripContentTypeParams drops RFC 7231 parameters such as charset, returning
// the bare type/subtype. Empty input stays empty.
func StripContentTypeParams(ct string) string {
	ct = strings.TrimSpace(ct)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	return ct
}

// SniffSupportedImageMIME returns image/jpeg|png|gif|webp when magic bytes
// match, otherwise "". The result never includes charset parameters.
func SniffSupportedImageMIME(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	// DetectContentType's WEBP signature is stricter in recent Go; RIFF....WEBP
	// is enough to identify a WebP container (some clients mislabel these as jpeg).
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	detected := strings.ToLower(StripContentTypeParams(http.DetectContentType(data)))
	switch detected {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return detected
	default:
		return ""
	}
}
