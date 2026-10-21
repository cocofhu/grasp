package engine

import (
	"errors"

	"github.com/cocofhu/grasp/internal/models"
)

// ErrClarifyVisitEnded is returned when a clarify turn is refused because its
// visit already ended (run cancelled / failed / completed, or ResumeFrom opened
// a newer visit). Match with errors.Is. HTTP maps it to 409 and WS to an error
// frame, both with code "clarify_visit_ended".
var ErrClarifyVisitEnded = errors.New("clarify_visit_ended")

// ClarifyVisitEndedMessage is the fixed human-readable text. The frontend
// builds the resend / auto-resend hint from AutoRetryOnResume.
const ClarifyVisitEndedMessage = "运行已结束，这条消息没有发送"

// ClarifyVisitEndedError is the structured form of ErrClarifyVisitEnded.
type ClarifyVisitEndedError struct {
	// AutoRetryOnResume is true for the system first message that ResumeFrom
	// redelivers; false for an ordinary user message.
	AutoRetryOnResume bool
	NodeID            string
	Iteration         int
	// Dropped echoes what the sender submitted so the client can refill its
	// draft. Nil for Live requests. Empty (no text) for the system first
	// message, which no client typed.
	Dropped *DroppedTurn
	// ClientMessageID is the optional id the sender attached to the turn,
	// echoed on every frame / response so the sender can match its local copy.
	ClientMessageID string
}

// DroppedTurn is the refused turn as the sender submitted it.
type DroppedTurn struct {
	ClientMessageID string
	Text            string
	Images          []models.PromptImage
	Annotations     []models.ReactAnnotation
}

func (e *ClarifyVisitEndedError) Error() string { return ClarifyVisitEndedMessage }

func (e *ClarifyVisitEndedError) Is(target error) bool { return target == ErrClarifyVisitEnded }

// DroppedPayload renders Dropped for HTTP / WS: text is present for user
// turns (even when empty) and omitted for the auto-retried first message;
// attachments / annotations only when non-empty.
func (e *ClarifyVisitEndedError) DroppedPayload() map[string]any {
	if e.Dropped == nil {
		return nil
	}
	out := map[string]any{}
	if !e.AutoRetryOnResume {
		out["text"] = e.Dropped.Text
		if len(e.Dropped.Images) > 0 {
			out["attachments"] = e.Dropped.Images
		}
		if len(e.Dropped.Annotations) > 0 {
			out["annotations"] = e.Dropped.Annotations
		}
	}
	return out
}

// FrameExtra is the field set added to a WS error frame (alongside the
// existing type/event/runId/nodeId/message). withDropped is only for replies
// that reach the sender alone (the WS enqueue reject); frames broadcast to
// every run subscriber never carry the dropped content.
func (e *ClarifyVisitEndedError) FrameExtra(withDropped bool) map[string]any {
	out := map[string]any{
		"message":           ClarifyVisitEndedMessage,
		"code":              "clarify_visit_ended",
		"autoRetryOnResume": e.AutoRetryOnResume,
		"iteration":         e.Iteration,
	}
	if e.NodeID != "" {
		out["nodeId"] = e.NodeID
	}
	if e.ClientMessageID != "" {
		out["clientMessageId"] = e.ClientMessageID
	}
	if withDropped {
		if d := e.DroppedPayload(); d != nil {
			out["dropped"] = d
		}
	}
	return out
}

// errIfClarifyVisitEnded returns *ClarifyVisitEndedError when the node's
// latest conversation visit can no longer take a turn. Callers must already
// know nodeID is a clarify node. A missing conversation returns nil so the
// caller keeps its own error.
func (e *Engine) errIfClarifyVisitEnded(runID, nodeID string, autoRetryOnResume bool, dropped *DroppedTurn) error {
	var conv models.ReactConversation
	if err := e.db.Select("iteration").Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&conv).Error; err != nil {
		return nil
	}
	if e.clarifyVisitLive(runID, nodeID, conv.Iteration) {
		return nil
	}
	ve := &ClarifyVisitEndedError{AutoRetryOnResume: autoRetryOnResume, NodeID: nodeID, Iteration: conv.Iteration, Dropped: dropped}
	if dropped != nil {
		ve.ClientMessageID = dropped.ClientMessageID
	}
	return ve
}

// nodeIsClarify reads only the run graph (not the full exec context) to tell
// whether nodeID is a clarify node.
func (e *Engine) nodeIsClarify(runID, nodeID string) bool {
	var run models.Run
	if err := e.db.Select("id", "graph").First(&run, "id = ?", runID).Error; err != nil {
		return false
	}
	n := run.Graph.FindNode(nodeID)
	return n != nil && n.Caps.Clarify()
}

// publishClarifyVisitEnded broadcasts the visit-ended error frame on the run
// broker (used for the server-side first message, which no client sent).
func (e *Engine) publishClarifyVisitEnded(runID string, ve *ClarifyVisitEndedError) {
	e.publishReview(runID, ve.NodeID, "error", ve.FrameExtra(false))
}
