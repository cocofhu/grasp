package runtime

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"
)

// toInt coerces a config value into an int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// renderTranscript flattens a react message history for output.
func renderTranscript(h []models.ReactMessage) string {
	var b strings.Builder
	for _, m := range h {
		b.WriteString(m.Role)
		b.WriteString(": ")
		b.WriteString(m.Text)
		b.WriteString("\n")
	}
	return b.String()
}
