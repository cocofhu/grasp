package provider

import (
	"log"
	"strings"
	"sync"
)

// OutputTail keeps the last Max bytes a child process wrote, so a crash
// surfaces a diagnostic instead of a bare exit status. With a LogPrefix each
// line is also mirrored to the log, redacted against Env.
type OutputTail struct {
	Max       int
	LogPrefix string
	// Env is the child's environment; its credential values never reach the log.
	Env []string

	mu   sync.Mutex
	data []byte
	// partial is the unfinished last line, so a secret split across writes
	// is still whole when it is redacted.
	partial []byte
}

// maxLogLine bounds how much of an unfinished line is held back; past it
// the line is logged without waiting for its end.
const maxLogLine = 4096

func (t *OutputTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.LogPrefix != "" {
		t.partial = append(t.partial, p...)
		lines := strings.Split(string(t.partial), "\n")
		last := lines[len(lines)-1]
		lines = lines[:len(lines)-1]
		if len(last) > maxLogLine {
			lines, last = append(lines, last), ""
		}
		t.partial = append(t.partial[:0], last...)
		for _, line := range lines {
			if line = strings.TrimSpace(RedactDiagnostic(line, t.Env)); line != "" {
				if len(line) > 500 {
					line = line[:500] + "…"
				}
				log.Printf("%s: %s", t.LogPrefix, line)
			}
		}
	}
	t.data = append(t.data, p...)
	if over := len(t.data) - t.Max; over > 0 {
		t.data = t.data[over:]
	}
	return len(p), nil
}

// String is the kept output without surrounding whitespace.
func (t *OutputTail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.data))
}
