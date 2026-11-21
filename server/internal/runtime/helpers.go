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
		writeSteers(&b, m)
		b.WriteString(m.Role)
		b.WriteString(": ")
		b.WriteString(m.Text)
		b.WriteString("\n")
	}
	return b.String()
}

// writeSteers renders the input an agent reply took mid-turn as the human
// lines it was, ahead of the reply that answered it.
func writeSteers(b *strings.Builder, m models.ReactMessage) {
	if m.Role == "human" {
		return
	}
	for _, s := range models.SteerTexts(m.Parts) {
		b.WriteString("human: ")
		b.WriteString(s)
		b.WriteString("\n")
	}
}
