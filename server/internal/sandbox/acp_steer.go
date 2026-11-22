package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cocofhu/grasp/internal/models"
)

// SteerStatus is the bridge's answer to a steer request (op:steer_ack).
type SteerStatus string

const (
	// SteerDelivered: the running turn took the input. A user_steer event in
	// that turn's stream records it.
	SteerDelivered SteerStatus = "delivered"
	// SteerUnsupported: the agent session cannot take input mid-turn.
	SteerUnsupported SteerStatus = "unsupported"
	// SteerNoTurn: no turn of this client is running any more.
	SteerNoTurn SteerStatus = "no_turn"
	// SteerFailed: delivery was attempted and failed.
	SteerFailed SteerStatus = "failed"
)

// steerAckWait bounds the wait for op:steer_ack. A var so tests can shrink it.
var steerAckWait = 30 * time.Second

type steerAck struct {
	Op      string      `json:"op"`
	SteerID string      `json:"steerId"`
	Status  SteerStatus `json:"status"`
	Message string      `json:"message"`
}

// SupportsSteer reports whether the connected agent session takes input into
// a running turn (capabilities.steer of the bridge's connected frame).
func (c *ACPClient) SupportsSteer() bool { return c.steerable.Load() }

// Steer delivers text (and attachments) into the turn this client has in
// flight. A status other than SteerDelivered means the input did not reach
// the turn and the caller still owns it; SteerFailed comes with the reason.
func (c *ACPClient) Steer(ctx context.Context, text string, images []models.PromptImage) (SteerStatus, error) {
	if !c.IsConnected() {
		return "", fmt.Errorf("%w: not connected", ErrConnClosed)
	}
	opID := c.InFlightOpID()
	if opID == "" {
		return SteerNoTurn, nil
	}
	if !c.SupportsSteer() {
		return SteerUnsupported, nil
	}
	images, err := c.prepareImages(ctx, images)
	if err != nil {
		return SteerFailed, err
	}
	steerID := "s-" + uuid.NewString()
	ack := make(chan steerAck, 1)
	c.steerMu.Lock()
	if c.steerWaits == nil {
		c.steerWaits = map[string]chan steerAck{}
	}
	c.steerWaits[steerID] = ack
	c.steerMu.Unlock()
	defer func() {
		c.steerMu.Lock()
		delete(c.steerWaits, steerID)
		c.steerMu.Unlock()
	}()

	msg := map[string]any{"op": "steer", "opId": opID, "steerId": steerID, "text": text}
	attachImages(msg, images)
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	if err := c.send(msg); err != nil {
		return "", fmt.Errorf("%w: send steer: %v", ErrConnClosed, err)
	}
	timer := time.NewTimer(steerAckWait)
	defer timer.Stop()
	select {
	case a := <-ack:
		if a.Status == SteerFailed {
			return SteerFailed, errors.New(firstNonEmpty(a.Message, "steer failed"))
		}
		return a.Status, nil
	case <-done:
		return "", fmt.Errorf("%w: connection closed", ErrConnClosed)
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
		return "", fmt.Errorf("steer ack timeout (%s)", steerAckWait)
	}
}

// observeSteerAck resolves a pending Steer. Acks are broadcast to every
// client of the chat; one without a waiter here belongs to another client.
func (c *ACPClient) observeSteerAck(raw []byte) {
	var a steerAck
	if json.Unmarshal(raw, &a) != nil || a.Op != "steer_ack" || a.SteerID == "" {
		return
	}
	c.steerMu.Lock()
	ch := c.steerWaits[a.SteerID]
	c.steerMu.Unlock()
	if ch != nil {
		select {
		case ch <- a:
		default:
		}
	}
}

// observeConnected records the session capabilities a connected frame
// announces. A bridge that announces none cannot steer.
func (c *ACPClient) observeConnected(raw []byte) {
	var m struct {
		Op           string `json:"op"`
		Capabilities struct {
			Steer bool `json:"steer"`
		} `json:"capabilities"`
	}
	if json.Unmarshal(raw, &m) != nil || m.Op != "connected" {
		return
	}
	c.steerable.Store(m.Capabilities.Steer)
}

// observeBridgeFrame keeps client state that does not belong to any one
// consumer of eventCh. Called from readLoop for every frame, so it must stay
// cheap for the common event frames.
func (c *ACPClient) observeBridgeFrame(raw []byte) {
	switch {
	case bytes.Contains(raw, []byte(`"queue_state"`)):
		c.observeQueueState(raw)
	case bytes.Contains(raw, []byte(`"steer_ack"`)):
		c.observeSteerAck(raw)
	case bytes.Contains(raw, []byte(`"connected"`)):
		c.observeConnected(raw)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
