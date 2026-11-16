// Package openclaw implements a dedicated one-shot codec for the OpenClaw CLI
// (`openclaw agent --local --json --session-id <id> --message <prompt>`). Its
// dominant output shape is a single pretty-printed JSON result document
// (payloads + meta) rather than line-delimited JSON, so the codec parses the
// whole stdout buffer at once (WholeOutputCodec), falling back to an NDJSON
// event scan for forward compatibility.
package openclaw

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"backend/internal/provider"
	"backend/internal/provider/oneshot"
)

var sessionCounter uint64

// randomToken returns a process-unique token for seeding a session id.
func randomToken() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatUint(atomic.AddUint64(&sessionCounter, 1), 36)
}

// Config parameterizes the OpenClaw codec.
type Config struct {
	AgentName  provider.Name
	Bin        string
	Runtime    string
	ConfigRoot string
	AuthEnvFn  func(env []string) []string
	Catalog    []provider.Model
}

// New builds a one-shot provider for the OpenClaw CLI.
func New(c Config) provider.Provider {
	return oneshot.NewProvider(&codec{CLI: oneshot.NewCLI(oneshot.CLIConfig{
		AgentName: c.AgentName, Bin: c.Bin, Runtime: c.Runtime, ConfigRoot: c.ConfigRoot,
		AuthEnvFn: c.AuthEnvFn, Catalog: c.Catalog,
	}), c: c})
}

type codec struct {
	oneshot.CLI
	c Config
}

// InitSession allocates a stable session id up front so multi-turn continuity
// survives even when the result document does not echo one back.
func (d *codec) InitSession(provider.OpenOptions) (string, error) {
	return "sbx-" + randomToken(), nil
}

// Args builds `openclaw agent --local --json --session-id <id> [--agent <model>]
// --message <prompt>`. OpenClaw binds the underlying model at agent-registration
// time, so opts.Model is treated as an --agent id, not a --model.
func (d *codec) Args(opts provider.OpenOptions, prompt, resumeID string) []string {
	args := []string{d.c.Bin, "agent", "--local", "--json"}
	if resumeID != "" {
		args = append(args, "--session-id", resumeID)
	}
	if opts.Model != "" && opts.Model != "auto" && !containsFlag(opts.CustomArgs, "--agent") {
		args = append(args, "--agent", opts.Model)
	}
	args = append(args, opts.CustomArgs...)
	args = append(args, "--message", prompt)
	return args
}

// ParseLine satisfies Codec but is unused: the engine drives OpenClaw through
// ParseAll (WholeOutputCodec).
func (d *codec) ParseLine(line []byte) oneshot.ParseResult { return oneshot.ParseResult{} }

// ParseAll parses the entire stdout buffer. It first tries the whole buffer as
// a single result document, then strips any leading log preamble, and finally
// falls back to scanning NDJSON streaming events.
func (d *codec) ParseAll(buf []byte) oneshot.ParseResult {
	trimmed := strings.TrimSpace(string(buf))
	if trimmed == "" {
		return oneshot.ParseResult{}
	}
	if res, ok := parseWholeResult(trimmed); ok {
		return buildFromResult(res)
	}
	lines := strings.Split(trimmed, "\n")
	for i, line := range lines {
		if len(line) > 0 && line[0] == '{' {
			if res, ok := parseWholeResult(strings.TrimSpace(strings.Join(lines[i:], "\n"))); ok {
				return buildFromResult(res)
			}
			break
		}
	}
	return parseNDJSON(trimmed)
}

// ── whole-document result path ──────────────────────────────────────────────

func parseWholeResult(raw string) (result, bool) {
	if len(raw) == 0 || raw[0] != '{' {
		return result{}, false
	}
	var res result
	if json.Unmarshal([]byte(raw), &res) != nil {
		return result{}, false
	}
	if res.Payloads == nil && res.Meta.DurationMs == 0 {
		return result{}, false
	}
	return res, true
}

func buildFromResult(res result) oneshot.ParseResult {
	var r oneshot.ParseResult
	for _, p := range res.Payloads {
		if p.Text != "" {
			r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindText, Text: p.Text})
		}
	}
	model := "openclaw"
	if res.Meta.AgentMeta != nil {
		if sid, ok := res.Meta.AgentMeta["sessionId"].(string); ok && sid != "" {
			r.SessionID = sid
		}
		if m, ok := res.Meta.AgentMeta["model"].(string); ok && strings.TrimSpace(m) != "" {
			model = strings.TrimSpace(m)
		}
		if u, ok := res.Meta.AgentMeta["usage"].(map[string]any); ok {
			if tu := parseUsage(u); nonZero(tu) {
				r.Usage = map[string]provider.TokenUsage{model: tu}
			}
		}
	}
	return r
}

// ── NDJSON fallback path ────────────────────────────────────────────────────

func parseNDJSON(buf string) oneshot.ParseResult {
	var r oneshot.ParseResult
	model := "openclaw"
	sc := bufio.NewScanner(bytes.NewReader([]byte(buf)))
	sc.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			log.Printf("openclaw: skip non-json line: %v (snippet=%q)", err, truncateForLog([]byte(line), 120))
			continue
		}
		if ev.Type == "" {
			continue
		}
		if ev.SessionID != "" {
			r.SessionID = ev.SessionID
		}
		switch ev.Type {
		case "text":
			if ev.Text != "" {
				r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindText, Text: ev.Text})
			}
		case "tool_use":
			r.Msgs = append(r.Msgs, oneshot.Msg{
				Kind:       oneshot.KindToolUse,
				ToolCallID: ev.CallID,
				ToolTitle:  ev.Tool,
				RawInput:   nonEmptyRaw(ev.Input),
			})
		case "tool_result":
			r.Msgs = append(r.Msgs, oneshot.Msg{
				Kind:       oneshot.KindToolResult,
				ToolCallID: ev.CallID,
				Text:       ev.Text,
			})
		case "error", "lifecycle":
			if ev.Type == "lifecycle" && ev.Phase != "error" && ev.Phase != "failed" && ev.Phase != "cancelled" {
				continue
			}
			r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindError, Text: ev.errorMessage()})
			r.StopReason = "failed"
		case "step_finish":
			if len(ev.Usage) > 0 {
				if tu := parseUsage(ev.Usage); nonZero(tu) {
					r.Usage = mergeUsage(r.Usage, model, tu)
				}
			}
		}
	}
	return r
}

// ── usage helpers ───────────────────────────────────────────────────────────

func parseUsage(data map[string]any) provider.TokenUsage {
	return provider.TokenUsage{
		InputTokens:      int64FirstOf(data, "input", "inputTokens", "input_tokens"),
		OutputTokens:     int64FirstOf(data, "output", "outputTokens", "output_tokens"),
		CacheReadTokens:  int64FirstOf(data, "cacheRead", "cachedInputTokens", "cached_input_tokens", "cache_read", "cache_read_input_tokens"),
		CacheWriteTokens: int64FirstOf(data, "cacheWrite", "cacheCreationInputTokens", "cache_creation_input_tokens", "cache_write"),
	}
}

func int64FirstOf(data map[string]any, keys ...string) int64 {
	for _, k := range keys {
		if v, ok := data[k]; ok {
			switch n := v.(type) {
			case float64:
				if int64(n) != 0 {
					return int64(n)
				}
			case int64:
				if n != 0 {
					return n
				}
			}
		}
	}
	return 0
}

func nonZero(u provider.TokenUsage) bool {
	return u.InputTokens != 0 || u.OutputTokens != 0 || u.CacheReadTokens != 0 || u.CacheWriteTokens != 0
}

func mergeUsage(m map[string]provider.TokenUsage, model string, u provider.TokenUsage) map[string]provider.TokenUsage {
	if m == nil {
		m = map[string]provider.TokenUsage{}
	}
	cur := m[model]
	m[model] = provider.TokenUsage{
		InputTokens:      cur.InputTokens + u.InputTokens,
		OutputTokens:     cur.OutputTokens + u.OutputTokens,
		CacheReadTokens:  cur.CacheReadTokens + u.CacheReadTokens,
		CacheWriteTokens: cur.CacheWriteTokens + u.CacheWriteTokens,
	}
	return m
}

func containsFlag(args []string, flag string) bool {
	prefix := flag + "="
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

func nonEmptyRaw(r json.RawMessage) json.RawMessage {
	if len(r) == 0 || string(r) == "null" {
		return nil
	}
	return r
}

// ── event/result schema ─────────────────────────────────────────────────────

type result struct {
	Payloads []payload `json:"payloads"`
	Meta     meta      `json:"meta"`
}

type payload struct {
	Text string `json:"text"`
}

type meta struct {
	DurationMs int64          `json:"durationMs"`
	AgentMeta  map[string]any `json:"agentMeta"`
}

type event struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId,omitempty"`
	Text      string          `json:"text,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	CallID    string          `json:"callId,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Usage     map[string]any  `json:"usage,omitempty"`
	Phase     string          `json:"phase,omitempty"`
	Error     *errObj         `json:"error,omitempty"`
	Message   string          `json:"message,omitempty"`
}

func (e event) errorMessage() string {
	if e.Error != nil {
		if m := e.Error.message(); m != "" {
			return m
		}
	}
	if e.Text != "" {
		return e.Text
	}
	if e.Message != "" {
		return e.Message
	}
	return "unknown openclaw error"
}

type errObj struct {
	Name    string   `json:"name,omitempty"`
	Data    *errData `json:"data,omitempty"`
	Message string   `json:"message,omitempty"`
}

func (e *errObj) message() string {
	if e.Data != nil && e.Data.Message != "" {
		return e.Data.Message
	}
	if e.Message != "" {
		return e.Message
	}
	return e.Name
}

type errData struct {
	Message string `json:"message,omitempty"`
}

func truncateForLog(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
