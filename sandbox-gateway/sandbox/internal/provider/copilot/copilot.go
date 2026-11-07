// Package copilot implements a dedicated one-shot codec for the Copilot CLI's
// JSONL event stream (`copilot -p <prompt> --output-format json`). Each stdout
// line is an envelope { "type": "dotted.event.name", "data": {...} } and the
// final line is a synthetic "result" event carrying the session id and exit
// code. The parser is stateful within a turn (active model + streamed-delta
// de-duplication), so it is produced fresh per turn via NewTurnParser.
package copilot

import (
	"context"
	"encoding/json"
	"log"

	"backend/internal/provider"
	"backend/internal/provider/oneshot"
)

// Config parameterizes the Copilot codec.
type Config struct {
	AgentName  provider.Name
	Bin        string
	Runtime    string
	ConfigRoot string
	AuthEnvFn  func(env []string) []string
	Catalog    []provider.Model
}

// New builds a one-shot provider for the Copilot CLI.
func New(c Config) provider.Provider { return oneshot.NewProvider(&codec{c: c}) }

type codec struct{ c Config }

func (d *codec) AgentName() provider.Name { return d.c.AgentName }
func (d *codec) Bin() string              { return d.c.Bin }
func (d *codec) Runtime() string          { return d.c.Runtime }
func (d *codec) ConfigRoot() string       { return d.c.ConfigRoot }
func (d *codec) ReportsUsage() bool       { return true }
func (d *codec) PromptViaStdin() bool     { return false }

func (d *codec) AuthEnv(env []string) []string {
	if d.c.AuthEnvFn == nil {
		return env
	}
	return d.c.AuthEnvFn(env)
}

func (d *codec) Models(context.Context) ([]provider.Model, error) { return d.c.Catalog, nil }

// Args builds `copilot -p <prompt> --output-format json --allow-all
// --no-ask-user [--model <m>] [--resume <id>]`. --allow-all is full headless
// mode (tools + paths + URLs); --no-ask-user disables the interactive prompt.
func (d *codec) Args(opts provider.OpenOptions, prompt, resumeID string) []string {
	args := []string{d.c.Bin, "-p", prompt, "--output-format", "json", "--allow-all", "--no-ask-user"}
	if opts.Model != "" && opts.Model != "auto" {
		args = append(args, "--model", opts.Model)
	}
	if resumeID != "" {
		args = append(args, "--resume", resumeID)
	}
	args = append(args, opts.CustomArgs...)
	return args
}

// NewTurnParser returns a fresh stateful line parser for one turn.
func (d *codec) NewTurnParser(opts provider.OpenOptions) oneshot.LineParser {
	seed := opts.Model
	if seed == "" {
		seed = string(d.c.AgentName)
	}
	return &turn{activeModel: seed, deltaSeen: map[string]bool{}}
}

// ParseLine satisfies Codec; the engine uses NewTurnParser for real turns, so
// this stateless fallback only ever handles isolated probing.
func (d *codec) ParseLine(line []byte) oneshot.ParseResult {
	return (&turn{activeModel: string(d.c.AgentName), deltaSeen: map[string]bool{}}).ParseLine(line)
}

// ── per-turn parser ────────────────────────────────────────────────────────

type turn struct {
	activeModel string
	deltaSeen   map[string]bool // messageId -> already streamed via deltas
	usageSeen   map[string]int64
}

func (t *turn) ParseLine(line []byte) oneshot.ParseResult {
	var ev event
	if err := json.Unmarshal(line, &ev); err != nil {
		log.Printf("copilot: skip non-json line: %v (snippet=%q)", err, truncateForLog(line, 120))
		return oneshot.ParseResult{}
	}
	var r oneshot.ParseResult

	switch ev.Type {
	case "session.start":
		var s sessionStart
		if json.Unmarshal(ev.Data, &s) == nil {
			if s.SelectedModel != "" {
				t.activeModel = s.SelectedModel
			}
			if s.SessionID != "" {
				r.SessionID = s.SessionID
			}
		}

	case "assistant.message_delta":
		var d messageDelta
		if json.Unmarshal(ev.Data, &d) == nil && d.DeltaContent != "" {
			if d.MessageID != "" {
				t.deltaSeen[d.MessageID] = true
			}
			r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindText, Text: d.DeltaContent})
		}

	case "assistant.message":
		var m assistantMessage
		if json.Unmarshal(ev.Data, &m) != nil {
			return r
		}
		// Emit the authoritative content only when no deltas streamed it,
		// so a delta-streamed turn isn't duplicated.
		if m.Content != "" && !t.deltaSeen[m.MessageID] {
			r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindText, Text: m.Content})
		}
		if m.ReasoningText != "" {
			r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindThinking, Text: m.ReasoningText})
		}
		if m.OutputTokens > 0 {
			delta := m.OutputTokens
			if m.MessageID != "" {
				if t.usageSeen == nil {
					t.usageSeen = map[string]int64{}
				}
				delta = max(m.OutputTokens-t.usageSeen[m.MessageID], 0)
				t.usageSeen[m.MessageID] = max(m.OutputTokens, t.usageSeen[m.MessageID])
			}
			r.Usage = map[string]provider.TokenUsage{t.model(): {OutputTokens: delta}}
			r.UsagePartial = true
		}
		for _, tr := range m.ToolRequests {
			r.Msgs = append(r.Msgs, oneshot.Msg{
				Kind:       oneshot.KindToolUse,
				ToolCallID: tr.ToolCallID,
				ToolTitle:  tr.Name,
				RawInput:   nonEmptyRaw(tr.Arguments),
			})
		}

	case "assistant.reasoning", "assistant.reasoning_delta":
		var rs reasoning
		if json.Unmarshal(ev.Data, &rs) == nil {
			text := rs.Content
			if text == "" {
				text = rs.DeltaContent
			}
			if text != "" {
				r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindThinking, Text: text})
			}
		}

	case "tool.execution_complete":
		var tc toolExecComplete
		if json.Unmarshal(ev.Data, &tc) != nil {
			return r
		}
		if tc.Model != "" {
			t.activeModel = tc.Model
		}
		out := ""
		status := "completed"
		if tc.Success && tc.Result != nil {
			out = tc.Result.Content
		} else if !tc.Success {
			status = "failed"
			if tc.Error != nil {
				out = "Error: " + tc.Error.Message
			} else if tc.Result != nil {
				out = tc.Result.Content
			}
		}
		r.Msgs = append(r.Msgs, oneshot.Msg{
			Kind:       oneshot.KindToolResult,
			ToolCallID: tc.ToolCallID,
			ToolStatus: status,
			Text:       out,
		})

	case "session.error":
		var se sessionError
		if json.Unmarshal(ev.Data, &se) == nil && se.Message != "" {
			r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindError, Text: se.Message})
			r.StopReason = "failed"
		}

	case "session.warning":
		var sw sessionWarning
		if json.Unmarshal(ev.Data, &sw) == nil && sw.Message != "" {
			r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindLog, Text: sw.Message})
		}

	case "result":
		if ev.SessionID != "" {
			r.SessionID = ev.SessionID
		}
		if ev.ExitCode != 0 {
			r.StopReason = "failed"
		}
	}

	return r
}

func (t *turn) model() string {
	if t.activeModel != "" {
		return t.activeModel
	}
	return "copilot"
}

func nonEmptyRaw(r json.RawMessage) json.RawMessage {
	if len(r) == 0 || string(r) == "null" {
		return nil
	}
	return r
}

// ── event schema ────────────────────────────────────────────────────────────

type event struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
	// Top-level fields on the synthetic "result" event.
	SessionID string `json:"sessionId,omitempty"`
	ExitCode  int    `json:"exitCode,omitempty"`
}

type sessionStart struct {
	SessionID     string `json:"sessionId"`
	SelectedModel string `json:"selectedModel"`
}

type assistantMessage struct {
	MessageID     string        `json:"messageId"`
	Content       string        `json:"content"`
	ToolRequests  []toolRequest `json:"toolRequests"`
	OutputTokens  int64         `json:"outputTokens"`
	ReasoningText string        `json:"reasoningText,omitempty"`
}

type toolRequest struct {
	ToolCallID string          `json:"toolCallId"`
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments"`
}

type messageDelta struct {
	MessageID    string `json:"messageId"`
	DeltaContent string `json:"deltaContent"`
}

type toolExecComplete struct {
	ToolCallID string      `json:"toolCallId"`
	Model      string      `json:"model"`
	Success    bool        `json:"success"`
	Result     *toolResult `json:"result,omitempty"`
	Error      *toolError  `json:"error,omitempty"`
}

type toolResult struct {
	Content string `json:"content"`
}

type toolError struct {
	Message string `json:"message"`
}

type reasoning struct {
	Content      string `json:"content,omitempty"`
	DeltaContent string `json:"deltaContent,omitempty"`
}

type sessionError struct {
	Message string `json:"message"`
}

type sessionWarning struct {
	Message string `json:"message"`
}

func truncateForLog(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
