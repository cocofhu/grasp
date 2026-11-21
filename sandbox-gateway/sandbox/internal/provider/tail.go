package provider

import (
	"log"
	"strings"
	"sync"
)

// OutputTail keeps the last Max bytes a child process wrote, so a crash
// surfaces a diagnostic instead of a bare exit status. With a LogPrefix each
// line is also mirrored to the log.
type OutputTail struct {
	Max       int
	LogPrefix string

	mu   sync.Mutex
	data []byte
}

func (t *OutputTail) Write(p []byte) (int, error) {
	if t.LogPrefix != "" {
		for _, line := range strings.Split(string(p), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				if len(line) > 500 {
					line = line[:500] + "…"
				}
				log.Printf("%s: %s", t.LogPrefix, line)
			}
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
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
