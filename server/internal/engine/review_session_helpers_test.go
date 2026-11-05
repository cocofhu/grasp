package engine

import (
	"fmt"
	"time"
)

// waitReviewReadyForTest waits for the entire producer pump to settle. Ready
// alone becomes true before the terminal frame is published, so a test reading
// a dialogue at that point can still miss its ended turn.
func (e *Engine) waitReviewReadyForTest(runID, producerID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s := e.laneSession(runID, producerID, ""); s == nil || s.Idle() {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	w, thinking := e.ReviewSessionState(runID, producerID)
	return fmt.Errorf("review session not idle (waiting=%d thinking=%v)", w, thinking)
}
