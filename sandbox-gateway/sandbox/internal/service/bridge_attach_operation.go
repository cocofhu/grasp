package service

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
)

var ErrOperationUnconfirmed = errors.New("original Agent operation cannot be verified; no prompt was sent")

const maxOperationReplayBytes = 32 << 20
const maxOperationReplayEvents = 100000

// AttachOperation is deliberately separate from EnsureAgent: it cannot create,
// reset, upgrade MCP, or enqueue an Agent session. The operation log belongs to
// the same still-live native session and canonical workspace.
// NativeSessionID stays stable when a one-shot CLI first discovers its vendor
// thread ID. A new backend instance or explicit Agent session changes it.
func (b *Bridge) NativeSessionID() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.nativeInstanceID + "-" + strconv.FormatUint(b.agentGeneration, 10)
}

// OperationSession reads current native identity without creating/resetting a
// session. A model-settings HTTP call may have replaced the provider since the
// original websocket handshake; dispatch freezes this acknowledged identity.
func (b *Bridge) OperationSession() (map[string]any, error) {
	root, _, release, err := b.canonicalWorkspace("", "")
	if err != nil {
		return nil, err
	}
	defer release()
	b.mu.Lock()
	p := b.sess
	ok := sessionUsable(p) && b.workspaceSessionMatches(p, root)
	native := b.nativeInstanceID + "-" + strconv.FormatUint(b.agentGeneration, 10)
	b.mu.Unlock()
	if !ok {
		return nil, ErrOperationUnconfirmed
	}
	fingerprint := ""
	if binding := b.bindingManager(); binding != nil {
		view, err := binding.Head()
		if err != nil {
			return nil, err
		}
		fingerprint = view.Head.Reference.Fingerprint
	}
	return map[string]any{"op": "operation_session", "sessionId": p.SessionID(), "nativeSessionId": native, "cwd": p.CWD(), "workspaceFingerprint": fingerprint}, nil
}

func (b *Bridge) AttachOperation(id, session, workspace, fingerprint string) (map[string]any, error) {
	if id == "" || len(id) > 128 || session == "" || workspace == "" {
		return nil, ErrOperationUnconfirmed
	}
	root, _, release, err := b.canonicalWorkspace(workspace, workspace)
	if err != nil {
		return nil, err
	}
	defer release()
	if binding := b.bindingManager(); binding != nil {
		view, err := binding.Head()
		if err != nil || fingerprint == "" || view.Head.Reference.Fingerprint != fingerprint {
			return nil, ErrOperationUnconfirmed
		}
	} else if fingerprint != "" {
		return nil, ErrOperationUnconfirmed
	}
	b.mu.Lock()
	p := b.sess
	ok := sessionUsable(p) && b.nativeInstanceID+"-"+strconv.FormatUint(b.agentGeneration, 10) == session && filepath.Clean(p.CWD()) == filepath.Clean(workspace) && b.workspaceSessionMatches(p, root)
	b.mu.Unlock()
	if !ok {
		return nil, ErrOperationUnconfirmed
	}
	frames, complete, err := b.evLog.operation(id)
	if err != nil {
		return nil, err
	}
	state := b.queueSnapshot()
	if running, ok := state["running"].(map[string]any); ok && running["opId"] != id {
		return nil, ErrOperationUnconfirmed
	}
	if state["queue_length"] != 0 {
		return nil, ErrOperationUnconfirmed
	}
	if !complete && state["busy"] != true {
		return nil, ErrOperationUnconfirmed
	}
	b.mu.Lock()
	ok = b.sess == p && sessionUsable(p)
	b.mu.Unlock()
	if !ok {
		return nil, ErrOperationUnconfirmed
	}
	return map[string]any{"op": "operation_attached", "opId": id, "sessionId": p.SessionID(), "nativeSessionId": session, "cwd": workspace, "workspaceFingerprint": fingerprint, "events": frames, "complete": complete}, nil
}

func (l *eventLog) operation(id string) ([]json.RawMessage, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var result []json.RawMessage
	started, done, size := false, false, 0
	for _, raw := range l.entries {
		var frame struct {
			Type string `json:"type"`
			ID   string `json:"opId"`
		}
		if json.Unmarshal(raw, &frame) != nil {
			return nil, false, ErrOperationUnconfirmed
		}
		if frame.ID != id {
			if done && frame.Type == "prompt_begin" {
				return nil, false, ErrOperationUnconfirmed
			}
			if started && !done {
				return nil, false, ErrOperationUnconfirmed
			}
			continue
		}
		if done || !started && frame.Type != "prompt_begin" || started && frame.Type == "prompt_begin" {
			return nil, false, ErrOperationUnconfirmed
		}
		started = true
		size += len(raw)
		if size > maxOperationReplayBytes || len(result) >= maxOperationReplayEvents {
			return nil, false, ErrOperationUnconfirmed
		}
		result = append(result, append(json.RawMessage(nil), raw...))
		done = frame.Type == "prompt_done"
	}
	if !started {
		return nil, false, ErrOperationUnconfirmed
	}
	return result, done, nil
}
