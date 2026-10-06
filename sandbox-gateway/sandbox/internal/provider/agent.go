package provider

import (
	"context"
	"encoding/json"
)

// Name identifies which agent CLI to drive.
type Name string

const (
	Cursor      Name = "cursor"
	ClaudeCode  Name = "claude_code"
	CodeBuddy   Name = "codebuddy"
	Trae        Name = "trae"
	Kiro        Name = "kiro"
	Qoder       Name = "qoder"
	Grok        Name = "grok"
	Kimi        Name = "kimi"
	Hermes      Name = "hermes"
	Codex       Name = "codex"
	Gemini      Name = "gemini"
	OpenCode    Name = "opencode"
	DevEco      Name = "deveco"
	Copilot     Name = "copilot"
	OpenClaw    Name = "openclaw"
	Antigravity Name = "antigravity"
	Pi          Name = "pi"

	// *_acp are opt-in fallbacks that force the long-lived JSON-RPC-over-stdio
	// transport for CLIs whose default here is one-shot stream-json. They are
	// not built as dedicated images by default; select via AGENT_PROVIDER when a
	// deployment prefers the stdio path.
	CursorACP     Name = "cursor_acp"
	ClaudeCodeACP Name = "claude_code_acp"
	CodeBuddyACP  Name = "codebuddy_acp"
)

// TransportKind distinguishes a long-lived session process from a one-shot CLI
// that is respawned per turn (resume-based multi-turn).
type TransportKind int

const (
	LongLived TransportKind = iota
	OneShot
)

func (t TransportKind) String() string {
	if t == OneShot {
		return "one-shot"
	}
	return "long-lived"
}

// PermissionChooser resolves a permission request (identical shape to the ACP
// chooser; transports that have no native permission model ignore it).
type PermissionChooser = func(ctx context.Context, rpcID json.RawMessage, rawParams json.RawMessage) (optionID string, err error)

// OpenOptions carries everything a provider needs to open a session.
type OpenOptions struct {
	Cwd             string
	FSRoot          string
	Model           string
	McpServers      json.RawMessage
	ResumeSessionID string // one-shot resume pointer; empty for a fresh session
	CustomArgs      []string
	AutoPermission  bool
}

// Model is one entry of a provider's model catalog.
type Model struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault,omitempty"`
}

// Provider is the factory + metadata for one agent CLI. It creates Sessions and
// describes how to inject config, without leaking transport details upward.
type Provider interface {
	Name() Name
	Runtime() string // capabilities.agent.runtime label
	DefaultConfigRoot() string
	Transport() TransportKind

	// Open starts (long-lived) or prepares (one-shot) a session. procCtx is
	// bound to the session lifetime; handshakeCtx bounds any startup RPC.
	Open(procCtx, handshakeCtx context.Context, opts OpenOptions,
		onEvent func(json.RawMessage), perm PermissionChooser) (Session, error)

	// ListModels returns the model catalog (static and/or dynamically probed);
	// an empty slice means "let the CLI decide" (auto).
	ListModels(ctx context.Context) ([]Model, error)

	// AuthEnv normalizes ACP_*/generic aliases into the CLI's native env vars.
	AuthEnv(env []string) []string
}
