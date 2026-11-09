package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cocofhu/grasp/internal/models"
	"strings"
	"time"
)

// OperationIdentity is a server-owned write-ahead receipt for one native
// prompt. It is SQL metadata, never a user-supplied execution capability.
type OperationIdentity struct {
	ID                   string `json:"id"`
	Attempt              int    `json:"attempt"`
	Resource             string `json:"resource"`
	SandboxID            uint   `json:"sandboxId"`
	Generation           uint64 `json:"generation"`
	SessionID            string `json:"sessionId"`
	Workspace            string `json:"workspace"`
	WorkspaceFingerprint string `json:"workspaceFingerprint,omitempty"`
	Authorization        string `json:"authorization"`
}

var ErrOperationUnconfirmed = errors.New("original native operation cannot be verified")

func (op OperationIdentity) Valid() bool {
	return ValidOperationID(op.ID) && op.Attempt > 0 && op.Resource != "" && op.SessionID != "" && op.Workspace != "" && op.Authorization != "" && len(op.Resource) <= 191 && len(op.SessionID) <= 128 && len(op.Workspace) <= 4096 && len(op.Authorization) <= 128 && len(op.WorkspaceFingerprint) <= 64 && !strings.ContainsRune(op.Workspace, 0)
}

func ValidOperationID(id string) bool {
	return strings.HasPrefix(id, "g-") && len(id) > 2 && len(id) <= 128 && !strings.ContainsAny(id, " \t\r\n\x00")
}

// WithAttachOperation opts into the native read-only attachment protocol. An
// ordinary connected frame is insufficient; old runtimes cannot silently
// fall back to EnsureAgent or resend a prompt.
func (c *ACPClient) WithAttachOperation(op OperationIdentity) *ACPClient {
	c.attachOperation = &op
	return c
}

func (c *ACPClient) NativeSessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nativeSessionID
}

func (c *ACPClient) WorkspaceIdentity() (string, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionWorkspace, c.workspaceFingerprint
}

func (c *ACPClient) observeConnectedWorkspace(raw json.RawMessage) {
	var value struct {
		NativeSession string `json:"nativeSessionId"`
		Cwd           string `json:"cwd"`
		Fingerprint   string `json:"workspaceFingerprint"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return
	}
	c.mu.Lock()
	c.sessionWorkspace, c.workspaceFingerprint, c.nativeSessionID = value.Cwd, value.Fingerprint, value.NativeSession
	c.mu.Unlock()
}

func (c *ACPClient) operationRequest() map[string]any {
	op := c.attachOperation
	return map[string]any{"op": "attach_operation", "opId": op.ID, "sessionId": op.SessionID, "cwd": op.Workspace, "workspaceFingerprint": op.WorkspaceFingerprint}
}

func (c *ACPClient) RefreshOperationSession(ctx context.Context) error {
	if c.attachOperation != nil {
		return ErrOperationUnconfirmed
	}
	release := c.acquireReader()
	defer release()
	c.drainEvents()
	if err := c.send(map[string]any{"op": "operation_session"}); err != nil {
		return err
	}
	for {
		select {
		case raw := <-c.eventCh:
			var reply struct {
				Op            string `json:"op"`
				NativeSession string `json:"nativeSessionId"`
				Workspace     string `json:"cwd"`
			}
			if json.Unmarshal(raw, &reply) != nil {
				continue
			}
			if reply.Op == "error" {
				return fmt.Errorf("%w: %s", ErrOperationUnconfirmed, parseErrorMessage(raw))
			}
			if reply.Op != "operation_session" {
				continue
			}
			if reply.NativeSession == "" || reply.Workspace == "" {
				return ErrOperationUnconfirmed
			}
			c.observeConnectedWorkspace(raw)
			return nil
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-c.done:
			return ErrConnClosed
		}
	}
}

type operationReply struct {
	Op            string            `json:"op"`
	ID            string            `json:"opId"`
	Session       string            `json:"sessionId"`
	NativeSession string            `json:"nativeSessionId"`
	Workspace     string            `json:"cwd"`
	Fingerprint   string            `json:"workspaceFingerprint"`
	Events        []json.RawMessage `json:"events"`
	Complete      bool              `json:"complete"`
}

func (c *ACPClient) checkOperationReply(raw json.RawMessage, complete bool) (*operationReply, error) {
	var reply operationReply
	if json.Unmarshal(raw, &reply) != nil {
		return nil, ErrOperationUnconfirmed
	}
	op := c.attachOperation
	if op == nil || reply.Op != "operation_attached" || reply.ID != op.ID || (reply.Session == "" || reply.NativeSession != op.SessionID) || reply.Workspace != op.Workspace || reply.Fingerprint != op.WorkspaceFingerprint || complete && !reply.Complete {
		return nil, fmt.Errorf("%w: operation/session/workspace or completion does not match", ErrOperationUnconfirmed)
	}
	if len(reply.Events) == 0 || len(reply.Events) > 100000 {
		return nil, ErrOperationUnconfirmed
	}
	size := 0
	for i, raw := range reply.Events {
		var frame struct {
			Type string `json:"type"`
			ID   string `json:"opId"`
		}
		if json.Unmarshal(raw, &frame) != nil || frame.ID != op.ID || i == 0 && frame.Type != "prompt_begin" || i > 0 && frame.Type == "prompt_begin" || frame.Type == "prompt_done" && i != len(reply.Events)-1 {
			return nil, fmt.Errorf("%w: invalid event identity or boundary at %d", ErrOperationUnconfirmed, i)
		}
		size += len(raw)
		if size > 32<<20 {
			return nil, ErrOperationUnconfirmed
		}
		if i == len(reply.Events)-1 && reply.Complete && frame.Type != "prompt_done" {
			return nil, ErrOperationUnconfirmed
		}
	}
	return &reply, nil
}

// ReattachOperation waits only for the persisted operation, then obtains a
// bounded complete turn from the same session. It never uses "first turn",
// partial pagination, or a prompt fallback.
func (c *ACPClient) ReattachOperation(ctx context.Context) (*ChatResult, error) {
	if c.attachOperation == nil {
		return nil, ErrOperationUnconfirmed
	}
	id := c.attachOperation.ID
	state, ok := c.WaitBridgeKnown(ctx, 15*time.Second)
	if !ok || state.RunningOpID != "" && state.RunningOpID != id || state.Waiting != 0 {
		return nil, fmt.Errorf("%w: queue state missing or belongs to another operation", ErrOperationUnconfirmed)
	}
	if state.RunningOpID == id {
		if err := c.AwaitTurn(ctx, id); err != nil {
			return nil, errors.Join(ErrOperationUnconfirmed, err)
		}
	}
	release := c.acquireReader()
	defer release()
	c.drainEvents()
	if err := c.send(c.operationRequest()); err != nil {
		return nil, errors.Join(ErrOperationUnconfirmed, err)
	}
	for {
		select {
		case raw := <-c.eventCh:
			var header struct {
				Op string `json:"op"`
			}
			_ = json.Unmarshal(raw, &header)
			if header.Op == "error" {
				return nil, fmt.Errorf("%w: %s", ErrOperationUnconfirmed, parseErrorMessage(raw))
			}
			if header.Op != "operation_attached" {
				continue
			}
			reply, err := c.checkOperationReply(raw, true)
			if err != nil {
				return nil, err
			}
			result, err := c.ReplayTurn(reply.Events)
			if result != nil {
				result.OpID = id
			}
			if err == nil {
				c.attachedComplete.Store(true)
			}
			return result, err
		case <-ctx.Done():
			return nil, errors.Join(ErrOperationUnconfirmed, context.Cause(ctx))
		case <-c.done:
			return nil, errors.Join(ErrOperationUnconfirmed, ErrConnClosed)
		}
	}
}

// ChatOperation uses an already persisted native ID. The normal Chat methods
// still allocate independent IDs for subsequent review/instruction turns.
func (c *ACPClient) ChatOperation(ctx context.Context, id, text string, images []models.PromptImage, progress func(*ChatResult)) (*ChatResult, error) {
	if c.attachOperation != nil || !ValidOperationID(id) {
		return nil, ErrOperationUnconfirmed
	}
	return c.runTurnID(ctx, id, text, images, nil, progress)
}
