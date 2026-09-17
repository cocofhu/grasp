// Package codex drives the Codex CLI in non-interactive JSON mode
// (`codex exec --json`). Current thread/turn/item events and legacy msg
// envelopes are mapped into the unified taxonomy. Multi-turn
// continuity uses `codex exec resume <session-id>`.
package codex

import (
	"context"
	"encoding/json"
	"log"

	"backend/internal/backend/common"
	"backend/internal/provider"
	"backend/internal/provider/oneshot"
)

const (
	bin        = "codex"
	runtime    = "codex-cli"
	configRoot = "/root/.codex"
)

// New returns the Codex one-shot provider.
func New() provider.Provider { return oneshot.NewProvider(&codec{}) }

type codec struct{}

func (codec) AgentName() provider.Name { return provider.Codex }
func (codec) Bin() string              { return bin }
func (codec) Runtime() string          { return runtime }
func (codec) ConfigRoot() string       { return configRoot }
func (codec) ReportsUsage() bool       { return true }
func (codec) PromptViaStdin() bool     { return false }

func (codec) AuthEnv(env []string) []string {
	return common.SetIfEmpty(env, "OPENAI_API_KEY",
		common.FirstNonEmptyEnv("ACP_CODEX_API_KEY", "CODEX_API_KEY", "OPENAI_API_KEY"))
}

func (codec) Models(context.Context) ([]provider.Model, error) { return nil, nil }

func (codec) Args(opts provider.OpenOptions, prompt, resumeID string) []string {
	args := []string{bin, "exec"}
	if resumeID != "" {
		args = append(args, "resume", resumeID)
	}
	args = append(args, "--json", "--skip-git-repo-check")
	if opts.Model != "" && opts.Model != "auto" {
		args = append(args, "--model", opts.Model)
	}
	if opts.AutoPermission {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	}
	args = append(args, opts.CustomArgs...)
	args = append(args, prompt)
	return args
}

type envelope struct {
	Msg *inner `json:"msg"`
	// Some builds place the fields at top level as well.
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
}

type inner struct {
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	Delta     string    `json:"delta"`
	Text      string    `json:"text"`
	Output    string    `json:"output"`
	SessionID string    `json:"session_id"`
	CallID    string    `json:"call_id"`
	Name      string    `json:"name"`
	Command   string    `json:"command"`
	Info      *tokenCnt `json:"info"`
}

type tokenCnt struct {
	InputTokens       int64 `json:"input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	TotalTokens       int64 `json:"total_tokens"`
}

func (codec) ParseLine(line []byte) oneshot.ParseResult {
	if result, ok := parseExecEvent(line); ok {
		return result
	}
	var e envelope
	if err := json.Unmarshal(line, &e); err != nil {
		log.Printf("codex: skip non-json line: %v", err)
		return oneshot.ParseResult{}
	}
	m := e.Msg
	if m == nil {
		m = &inner{Type: e.Type, SessionID: e.SessionID}
	}
	var res oneshot.ParseResult
	if m.SessionID != "" {
		res.SessionID = m.SessionID
	} else if e.SessionID != "" {
		res.SessionID = e.SessionID
	}

	switch m.Type {
	case "session_configured", "session_created":
		// session id captured above
	case "agent_message", "agent_message_delta":
		txt := m.Delta
		if txt == "" {
			txt = m.Message
		}
		if txt == "" {
			txt = m.Text
		}
		if txt != "" {
			res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindText, Text: txt})
		}
	case "agent_reasoning", "agent_reasoning_delta":
		txt := m.Delta
		if txt == "" {
			txt = m.Text
		}
		if txt != "" {
			res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindThinking, Text: txt})
		}
	case "exec_command_begin", "mcp_tool_call_begin", "patch_apply_begin":
		title := m.Name
		if title == "" {
			title = m.Command
		}
		if title == "" && m.Type == "patch_apply_begin" {
			title = "patch_apply"
		}
		res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindToolUse, ToolCallID: m.CallID, ToolTitle: title})
	case "exec_command_end", "mcp_tool_call_end", "patch_apply_end":
		out := m.Output
		if out == "" {
			out = m.Text
		}
		res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindToolResult, ToolCallID: m.CallID, Text: out})
	case "token_count":
		if m.Info != nil {
			res.Usage = map[string]provider.TokenUsage{"default": {
				InputTokens:     m.Info.InputTokens,
				OutputTokens:    m.Info.OutputTokens,
				CacheReadTokens: m.Info.CachedInputTokens,
			}}
		}
	case "error":
		res.StopReason = "failed"
		if m.Message != "" {
			res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindError, Text: m.Message})
		}
	case "task_complete", "turn_complete", "shutdown_complete":
		res.StopReason = "end_turn"
	case "turn_aborted":
		res.StopReason = "cancelled"
	}
	return res
}

func truncateForLog(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
