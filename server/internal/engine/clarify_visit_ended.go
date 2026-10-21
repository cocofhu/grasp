package engine

import (
	"errors"

	"github.com/cocofhu/grasp/internal/models"
)

// ErrClarifyVisitEnded is returned when a clarify turn is refused because its
// visit already ended (run cancelled / failed / completed, or ResumeFrom opened
// a newer visit). Match with errors.Is. The HTTP layer maps it to 409 with
// code "clarify_visit_ended".
var ErrClarifyVisitEnded = errors.New("clarify_visit_ended")

// ClarifyVisitEndedError is the structured form of ErrClarifyVisitEnded.
// AutoRetryOnResume is true for the system first message that ResumeFrom will
// redeliver; false for an ordinary user message (frontend builds the toast).
type ClarifyVisitEndedError struct {
	AutoRetryOnResume bool
}

func (e *ClarifyVisitEndedError) Error() string {
	return "运行已结束，这条消息没有发送"
}

func (e *ClarifyVisitEndedError) Is(target error) bool {
	return target == ErrClarifyVisitEnded
}

func clarifyVisitEndedErr(autoRetryOnResume bool) error {
	return &ClarifyVisitEndedError{AutoRetryOnResume: autoRetryOnResume}
}

// errIfClarifyVisitEnded returns ClarifyVisitEndedError when the node's latest
// clarify visit can no longer accept a turn. Non-clarify nodes and missing
// conversations return nil so existing callers keep their own errors.
func (e *Engine) errIfClarifyVisitEnded(runID, nodeID string, autoRetryOnResume bool) error {
	c, err := e.loadCtx(runID)
	if err != nil {
		return nil
	}
	n := c.graph.FindNode(nodeID)
	if n == nil || !n.Caps.Clarify() {
		return nil
	}
	var conv models.ReactConversation
	if err := e.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&conv).Error; err != nil {
		return nil
	}
	if e.clarifyVisitLive(runID, nodeID, conv.Iteration) {
		return nil
	}
	return clarifyVisitEndedErr(autoRetryOnResume)
}
