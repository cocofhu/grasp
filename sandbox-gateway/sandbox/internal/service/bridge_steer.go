package service

import (
	"errors"
	"log"
	"strings"
	"time"

	"backend/internal/provider"
)

// Steer ack statuses carried by op:steer_ack.
const (
	// SteerDelivered: the running turn took the input; a user_steer event in
	// that turn's stream records it.
	SteerDelivered = "delivered"
	// SteerUnsupported: this agent cannot take input mid-turn. The caller
	// interrupts the turn and runs the input as the next one.
	SteerUnsupported = "unsupported"
	// SteerNoTurn: the named turn is not running (finished, or never started).
	SteerNoTurn = "no_turn"
	// SteerFailed: delivery was attempted and failed; message says why.
	SteerFailed = "failed"
)

// SteerTurn delivers text (and attachments) into the running turn opID and
// broadcasts op:steer_ack. The returned status is that ack's status.
func (b *Bridge) SteerTurn(opID, steerID, text string, images []PromptImage) string {
	status, message := b.steer(strings.TrimSpace(opID), steerID, strings.TrimSpace(text), images)
	ack := map[string]any{"op": "steer_ack", "steerId": steerID, "turnOpId": opID, "status": status}
	if message != "" {
		ack["message"] = message
	}
	log.Printf("ws→acp %s oid=%s: steer %s status=%s", b.AgentLogPrefix(), opID, steerID, status)
	b.Broadcast(ack)
	return status
}

func (b *Bridge) steer(opID, steerID, text string, images []PromptImage) (status, message string) {
	if text == "" && len(images) == 0 {
		return SteerFailed, "empty message"
	}
	b.mu.Lock()
	sess := b.sess
	b.mu.Unlock()
	if !sessionUsable(sess) {
		return SteerNoTurn, ""
	}
	steerer, ok := sess.(provider.Steerer)
	if !ok || !sess.Capabilities().Steer {
		return SteerUnsupported, ""
	}
	if opID == "" || !b.turnRunning(opID) {
		return SteerNoTurn, ""
	}
	prompt := text
	if len(images) > 0 {
		files, err := provider.MaterializeAttachments(images)
		if err != nil {
			return SteerFailed, "附件保存失败: " + err.Error()
		}
		prompt = provider.AppendAttachmentRefs(prompt, files)
	}
	// steerMu keeps the next turn from starting until this write returns, so
	// the input reaches opID's turn or none.
	b.steerMu.Lock()
	defer b.steerMu.Unlock()
	b.turnMu.Lock()
	th := b.activeTurn
	b.turnMu.Unlock()
	if th == nil || th.opID != opID {
		return SteerNoTurn, ""
	}
	err := steerer.Steer(prompt)
	if err == nil {
		th.lastActivity.Store(time.Now().UnixNano())
	}
	if errors.Is(err, provider.ErrNoActiveTurn) {
		return SteerNoTurn, ""
	}
	if err != nil {
		return SteerFailed, err.Error()
	}
	data := map[string]any{"type": "user_steer", "sessionId": sess.SessionID(), "opId": opID, "steerId": steerID, "text": text}
	addAttachmentPreview(data, images)
	b.Broadcast(eventEnvelope(data, opID))
	return SteerDelivered, ""
}

func (b *Bridge) turnRunning(opID string) bool {
	b.turnMu.Lock()
	defer b.turnMu.Unlock()
	return b.activeTurn != nil && b.activeTurn.opID == opID
}
