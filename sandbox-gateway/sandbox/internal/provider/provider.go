// Package provider defines the transport-agnostic agent session abstraction the
// service layer depends on. It decouples the bridge from any single CLI protocol
// (JSON-RPC over stdio, stream-json, app-server, plain text, ...) so additional
// agent transports can be added without touching the WSP wire protocol.
package provider

import (
	"context"
	"errors"
)

// ErrTurnTimeout is the cancel cause the bridge watchdog attaches to a turn ctx
// it aborts (idle or total-duration limit). Transports that emit their own
// prompt_done report StopReasonTimeout when context.Cause(ctx) matches it.
var ErrTurnTimeout = errors.New("turn timeout")

// StopReasonTimeout is the prompt_done stopReason for a watchdog-aborted turn.
const StopReasonTimeout = "timeout"

// PromptImage is a base64 image/file attachment sent with a user turn.
type PromptImage struct {
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
	Name     string `json:"name,omitempty"`
}

// AgentInfo is the agent/session metadata surfaced in the connected payload and
// capability handshake.
type AgentInfo struct {
	Name    string
	Title   string
	Version string

	ModelID   string
	ModelName string
}

// TokenUsage is per-model token accounting (one turn, or cumulative). Unknown
// counters stay zero. The field names align with the protocol's reserved
// optional `usage` object.
type TokenUsage struct {
	InputTokens      int64 `json:"inputTokens,omitempty"`
	OutputTokens     int64 `json:"outputTokens,omitempty"`
	CacheReadTokens  int64 `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64 `json:"cacheWriteTokens,omitempty"`
}

// TurnResult summarizes one completed prompt turn.
type TurnResult struct {
	StopReason string
	// Usage is keyed by modelID; nil when the transport does not report usage.
	Usage map[string]TokenUsage
}

// Session is one live agent session held by the bridge. It takes the place of a
// direct dependency on any concrete transport handle (e.g. the ACP panel).
//
// Lifecycle: Done closes when the session is no longer usable. For a long-lived
// transport that means the subprocess exited; for a one-shot transport (spawn a
// fresh process per turn) it closes only on Close/Restart or a fatal error, so
// the session stays "connected" across turns.
type Session interface {
	SessionID() string
	CWD() string
	FSRoot() string
	Info() AgentInfo

	// Prompt runs one turn: it streams session/update frames via the event
	// callback supplied at open time and finishes with prompt_done. TurnResult
	// carries this turn's usage (nil when unavailable).
	Prompt(ctx context.Context, text string, images []PromptImage) (TurnResult, error)

	// ReportsUsage reports whether this session surfaces token usage (drives
	// capabilities.session.tokenUsage).
	ReportsUsage() bool
	// CumulativeUsage is the session-level running usage (may be carried on the
	// connected payload). nil when usage is not reported.
	CumulativeUsage() map[string]TokenUsage

	Cancel() error // cancel the in-flight turn
	Close() error  // end the session (restart / reconnect)

	// Done closes when the session is unusable (see lifecycle note above).
	Done() <-chan struct{}
	// ExitInfo reaps the session and returns the operator-facing exit banner.
	ExitInfo() (exitMsg string, err error)
}
