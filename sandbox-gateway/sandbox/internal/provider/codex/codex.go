// Package codex drives the Codex CLI in non-interactive JSON mode
// (`codex exec --json`). Codex wraps each event in a {"msg":{"type":...}}
// envelope; this codec unwraps it into the unified taxonomy. Multi-turn
// continuity uses `codex exec resume <session-id>`.
package codex

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"

	"backend/internal/provider"
	"backend/internal/provider/oneshot"
)

const (
	bin        = "codex"
	runtime    = "codex-cli"
	configRoot = "/root/.codex"
	// envReasoningEffort maps to Codex's model_reasoning_effort config key
	// (minimal / low / medium / high, depending on the model).
	envReasoningEffort = "ACP_BRIDGE_REASONING_EFFORT"
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

// AuthEnv does not inject an API key. Codex authenticates from CODEX_HOME/auth.json.
func (codec) AuthEnv(env []string) []string {
	if len(env) == 0 {
		return env
	}
	out := make([]string, 0, len(env))
	for _, item := range env {
		key, _, _ := strings.Cut(item, "=")
		switch key {
		case "OPENAI_API_KEY", "CODEX_API_KEY", "ACP_CODEX_API_KEY":
			continue
		default:
			out = append(out, item)
		}
	}
	return out
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
	if effort := strings.TrimSpace(os.Getenv(envReasoningEffort)); effort != "" && effort != "auto" {
		args = append(args, "-c", "model_reasoning_effort="+effort)
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
	var probe struct {
		Type string          `json:"type"`
		Msg  json.RawMessage `json:"msg"`
	}
	if err := json.Unmarshal(line, &probe); err != nil {
		log.Printf("codex: skip non-json line: %v (snippet=%q)", err, truncateForLog(line, 120))
		return oneshot.ParseResult{}
	}
	if len(probe.Msg) == 0 || string(probe.Msg) == "null" {
		if strings.Contains(probe.Type, ".") || probe.Type == "error" {
			return parseThreadLine(line)
		}
	}
	return parseLegacyLine(line)
}

func parseLegacyLine(line []byte) oneshot.ParseResult {
	var e envelope
	if err := json.Unmarshal(line, &e); err != nil {
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
			res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindError, Text: rewriteCodexAuthError(m.Message)})
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

// codexLoginRepaste is the user-facing text when ChatGPT login is rejected or
// cannot be refreshed. Keep it identical to the server-side message.
const codexLoginRepaste = "Codex 登录已失效或无法刷新，请重新粘贴登录文件（~/.codex/auth.json）"

func rewriteCodexAuthError(message string) string {
	if codexAuthRejected(message) {
		return codexLoginRepaste
	}
	return message
}

func codexAuthRejected(text string) bool {
	s := strings.ToLower(text)
	if s == "" {
		return false
	}
	if strings.Contains(text, codexLoginRepaste) || strings.Contains(s, "请重新粘贴登录文件") {
		return true
	}
	for _, p := range []string{
		"not logged in", "please log in", "please login",
		"failed to refresh", "could not refresh", "couldn't refresh",
		"unable to refresh", "invalid_grant", "re-authenticate", "reauthenticate",
		"authentication required", "authentication failed", "login required",
		"missing bearer",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	// codex-cli 0.160.1 reports a missing or rejected ChatGPT login as
	// "401 Unauthorized" against api.openai.com, not "please log in".
	if strings.Contains(s, "401 unauthorized") && strings.Contains(s, "api.openai.com") {
		return true
	}
	return false
}

type threadEvent struct {
	Type     string       `json:"type"`
	ThreadID string       `json:"thread_id"`
	Message  string       `json:"message"`
	Usage    *threadUsage `json:"usage"`
	Error    *threadErr   `json:"error"`
	Item     *threadItem  `json:"item"`
}

type threadUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
}

type threadErr struct {
	Message string `json:"message"`
}

type threadItem struct {
	ID               string       `json:"id"`
	Type             string       `json:"type"`
	Text             string       `json:"text"`
	Message          string       `json:"message"`
	Command          string       `json:"command"`
	AggregatedOutput string       `json:"aggregated_output"`
	Status           string       `json:"status"`
	Server           string       `json:"server"`
	Tool             string       `json:"tool"`
	Query            string       `json:"query"`
	Error            *threadErr   `json:"error"`
	Changes          []fileChange `json:"changes"`
}

type fileChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

func parseThreadLine(line []byte) oneshot.ParseResult {
	var ev threadEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return oneshot.ParseResult{}
	}
	var res oneshot.ParseResult
	switch ev.Type {
	case "thread.started":
		res.SessionID = ev.ThreadID
	case "turn.completed":
		res.StopReason = "end_turn"
		if ev.Usage != nil {
			res.Usage = map[string]provider.TokenUsage{"default": {
				InputTokens:      ev.Usage.InputTokens,
				OutputTokens:     ev.Usage.OutputTokens,
				CacheReadTokens:  ev.Usage.CachedInputTokens,
				CacheWriteTokens: ev.Usage.CacheWriteInputTokens,
			}}
		}
	case "turn.failed":
		res.StopReason = "failed"
		if msg := firstNonEmpty(errMessage(ev.Error), ev.Message); msg != "" {
			res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindError, Text: rewriteCodexAuthError(msg)})
		}
	case "error":
		res.StopReason = "failed"
		if msg := firstNonEmpty(ev.Message, errMessage(ev.Error)); msg != "" {
			res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindError, Text: rewriteCodexAuthError(msg)})
		}
	case "item.started":
		if ev.Item != nil && isCodexToolItem(ev.Item.Type) {
			res.Msgs = append(res.Msgs, codexToolUse(ev.Item))
		}
	case "item.completed":
		if ev.Item == nil {
			break
		}
		switch ev.Item.Type {
		case "agent_message":
			if ev.Item.Text != "" {
				res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindText, Text: ev.Item.Text})
			}
		case "reasoning":
			if ev.Item.Text != "" {
				res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindThinking, Text: ev.Item.Text})
			}
		case "error":
			msg := firstNonEmpty(ev.Item.Message, ev.Item.Text, errMessage(ev.Item.Error))
			if msg != "" {
				res.StopReason = "failed"
				res.Msgs = append(res.Msgs, oneshot.Msg{Kind: oneshot.KindError, Text: rewriteCodexAuthError(msg)})
			}
		default:
			if isCodexToolItem(ev.Item.Type) {
				if ev.Item.Type == "file_change" {
					res.Msgs = append(res.Msgs, codexToolUse(ev.Item))
				}
				res.Msgs = append(res.Msgs, codexToolResult(ev.Item))
			}
		}
	}
	return res
}

func isCodexToolItem(kind string) bool {
	switch kind {
	case "command_execution", "mcp_tool_call", "file_change", "web_search", "collab_tool_call":
		return true
	default:
		return false
	}
}

func codexToolUse(item *threadItem) oneshot.Msg {
	return oneshot.Msg{Kind: oneshot.KindToolUse, ToolCallID: item.ID, ToolTitle: codexToolTitle(item)}
}

func codexToolResult(item *threadItem) oneshot.Msg {
	status := "completed"
	switch strings.ToLower(item.Status) {
	case "failed", "declined":
		status = "failed"
	}
	text := item.AggregatedOutput
	if text == "" && item.Error != nil {
		text = item.Error.Message
	}
	if text == "" && item.Type == "file_change" {
		paths := make([]string, 0, len(item.Changes))
		for _, ch := range item.Changes {
			if ch.Path != "" {
				paths = append(paths, ch.Path)
			}
		}
		text = strings.Join(paths, "\n")
	}
	return oneshot.Msg{Kind: oneshot.KindToolResult, ToolCallID: item.ID, ToolTitle: codexToolTitle(item), Text: text, ToolStatus: status}
}

func codexToolTitle(item *threadItem) string {
	switch item.Type {
	case "command_execution":
		if item.Command != "" {
			return item.Command
		}
	case "mcp_tool_call":
		if item.Server != "" && item.Tool != "" {
			return item.Server + "/" + item.Tool
		}
		if item.Tool != "" {
			return item.Tool
		}
	case "web_search":
		if item.Query != "" {
			return item.Query
		}
	case "file_change":
		if len(item.Changes) > 0 && item.Changes[0].Path != "" {
			return item.Changes[0].Path
		}
		return "file_change"
	}
	if item.Command != "" {
		return item.Command
	}
	return item.Type
}

func errMessage(e *threadErr) string {
	if e == nil {
		return ""
	}
	return e.Message
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
