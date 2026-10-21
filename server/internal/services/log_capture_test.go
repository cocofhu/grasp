package services

import (
	"bytes"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// testLog is the package's global logger sink for tests. Tests used to swap
// log.Logger itself to capture output, which races with goroutines leaked by
// other tests (e.g. SandboxService.Open's async startContainer) that are
// still logging through log.Logger. Instead log.Logger is set exactly once,
// before any test goroutine runs, and captures redirect the sink's writer
// under a mutex.
var testLog = &logSink{w: os.Stderr}

func init() {
	log.Logger = zerolog.New(testLog).With().Timestamp().Logger()
}

type logSink struct {
	mu  sync.Mutex
	w   io.Writer
	buf *bytes.Buffer
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buf != nil {
		return s.buf.Write(p)
	}
	return s.w.Write(p)
}

// captureLog collects global log output until the test ends; read it with
// the returned func (safe while other goroutines keep logging).
func captureLog(t *testing.T) func() string {
	t.Helper()
	buf := &bytes.Buffer{}
	testLog.mu.Lock()
	testLog.buf = buf
	testLog.mu.Unlock()
	t.Cleanup(func() {
		testLog.mu.Lock()
		testLog.buf = nil
		testLog.mu.Unlock()
	})
	return func() string {
		testLog.mu.Lock()
		defer testLog.mu.Unlock()
		return buf.String()
	}
}
