// Package pi implements a dedicated one-shot codec for the Pi CLI's JSON event
// stream (`pi -p --mode json --session <path>`). Multi-turn continuity uses a
// session log file created up front and reused via --session; the file path is
// the resume pointer (Pi does not surface a session id on stdout). The parser
// is stateful within a turn (text sanitizer buffer + per-model usage), so it is
// produced fresh per turn via NewTurnParser.
package pi

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"backend/internal/provider"
	"backend/internal/provider/oneshot"
)

// Config parameterizes the Pi codec.
type Config struct {
	AgentName  provider.Name
	Bin        string
	Runtime    string
	ConfigRoot string
	// SessionDir is where per-session log files live (default: $HOME/.pi/sessions).
	SessionDir string
	AuthEnvFn  func(env []string) []string
	Catalog    []provider.Model
}

// New builds a one-shot provider for the Pi CLI.
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

// InitSession allocates a fresh session log file before the first turn. Pi
// refuses to start when --session points at a missing file, so the file is
// created empty here; the path becomes the resume pointer for every turn.
func (d *codec) InitSession(provider.OpenOptions) (string, error) {
	path, err := d.newSessionPath()
	if err != nil {
		return "", err
	}
	if err := ensureFile(path); err != nil {
		return "", err
	}
	return path, nil
}

// Args builds `pi -p --mode json --session <path> [--provider p] [--model m]
// <prompt>`. resumeID is the session file path (allocated by InitSession); as a
// safety net a fresh one is created when it is empty.
func (d *codec) Args(opts provider.OpenOptions, prompt, resumeID string) []string {
	sessionPath := resumeID
	if sessionPath == "" {
		if p, err := d.newSessionPath(); err == nil {
			sessionPath = p
		}
	}
	if sessionPath != "" {
		_ = ensureFile(sessionPath)
	}
	args := []string{d.c.Bin, "-p", "--mode", "json"}
	if sessionPath != "" {
		args = append(args, "--session", sessionPath)
	}
	if opts.Model != "" && opts.Model != "auto" {
		prov, model := splitModel(opts.Model)
		if prov != "" {
			args = append(args, "--provider", prov)
		}
		if model != "" {
			args = append(args, "--model", model)
		}
	}
	args = append(args, opts.CustomArgs...)
	args = append(args, prompt)
	return args
}

func (d *codec) sessionDir() (string, error) {
	if d.c.SessionDir != "" {
		return d.c.SessionDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pi", "sessions"), nil
}

func (d *codec) newSessionPath() (string, error) {
	dir, err := d.sessionDir()
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s.jsonl", time.Now().UTC().Format("20060102T150405.000000000"))
	return filepath.Join(dir, name), nil
}

func ensureFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

// splitModel parses "provider/model" into its parts; plain "model" passes
// through as (provider="", model="model").
func splitModel(s string) (prov, model string) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "/"); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return "", s
}

// NewTurnParser returns a fresh stateful line parser for one turn.
func (d *codec) NewTurnParser(opts provider.OpenOptions) oneshot.LineParser {
	return &turn{fallbackModel: opts.Model}
}

// ParseLine satisfies Codec; the engine uses NewTurnParser for real turns.
func (d *codec) ParseLine(line []byte) oneshot.ParseResult {
	return (&turn{}).ParseLine(line)
}

// ── per-turn parser ────────────────────────────────────────────────────────

// controlTokenRE strips Pi's inline tool-call control tokens (e.g. <|foo>bar,
// <name|>) that occasionally bleed into the assistant text stream.
var controlTokenRE = regexp.MustCompile(`<\|[A-Za-z0-9_-]+>[A-Za-z0-9_-]*|<[A-Za-z0-9_-]+\|>`)

type turn struct {
	fallbackModel string
	textBuf       string // holds a trailing partial that may complete a token
}

func (t *turn) ParseLine(line []byte) oneshot.ParseResult {
	var ev streamEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		log.Printf("pi: skip non-json line: %v (snippet=%q)", err, truncateForLog(line, 120))
		return oneshot.ParseResult{}
	}
	var r oneshot.ParseResult

	switch ev.Type {
	case "message_update":
		if ev.AssistantMessageEvent == nil {
			return r
		}
		switch ev.AssistantMessageEvent.Type {
		case "text_delta":
			if d := t.drain(ev.AssistantMessageEvent.Delta); d != "" {
				r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindText, Text: d})
			}
		case "thinking_delta":
			if d := ev.AssistantMessageEvent.Delta; d != "" {
				r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindThinking, Text: d})
			}
		}

	case "tool_execution_start":
		r.Msgs = append(r.Msgs, oneshot.Msg{
			Kind:       oneshot.KindToolUse,
			ToolCallID: ev.ToolCallID,
			ToolTitle:  ev.ToolName,
			RawInput:   nonEmptyRaw(ev.Args),
		})

	case "tool_execution_end":
		status := "completed"
		if ev.IsError {
			status = "failed"
		}
		r.Msgs = append(r.Msgs, oneshot.Msg{
			Kind:       oneshot.KindToolResult,
			ToolCallID: ev.ToolCallID,
			ToolStatus: status,
			Text:       decodeString(ev.Result),
		})

	case "turn_end":
		if flushed := t.flush(); flushed != "" {
			r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindText, Text: flushed})
		}
		if m := decodeMessage(ev.Message); m != nil && m.Usage != nil {
			model := firstNonEmpty(m.Model, t.fallbackModel, "unknown")
			r.Usage = map[string]provider.TokenUsage{model: {
				InputTokens:      m.Usage.Input,
				OutputTokens:     m.Usage.Output,
				CacheReadTokens:  m.Usage.CacheRead,
				CacheWriteTokens: m.Usage.CacheWrite,
			}}
		}

	case "error":
		msg := decodeString(ev.Message)
		r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindError, Text: msg})
		r.StopReason = "failed"

	case "auto_retry_end":
		if !ev.Success {
			r.StopReason = "failed"
			if ev.FinalError != "" {
				r.Msgs = append(r.Msgs, oneshot.Msg{Kind: oneshot.KindError, Text: ev.FinalError})
			}
		}
	}

	return r
}

// drain appends delta, strips complete control tokens, and emits everything
// except a trailing partial "<..." that might still complete into a token.
func (t *turn) drain(delta string) string {
	t.textBuf += delta
	cleaned := controlTokenRE.ReplaceAllString(t.textBuf, "")
	hold := 0
	if i := strings.LastIndexByte(cleaned, '<'); i >= 0 && !strings.ContainsRune(cleaned[i:], '>') {
		hold = len(cleaned) - i
	}
	emit := cleaned[:len(cleaned)-hold]
	t.textBuf = cleaned[len(cleaned)-hold:]
	return emit
}

func (t *turn) flush() string {
	s := controlTokenRE.ReplaceAllString(t.textBuf, "")
	t.textBuf = ""
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func nonEmptyRaw(r json.RawMessage) json.RawMessage {
	if len(r) == 0 || string(r) == "null" {
		return nil
	}
	return r
}

func decodeString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

func decodeMessage(raw json.RawMessage) *message {
	if len(raw) == 0 {
		return nil
	}
	var m message
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return &m
}

// ── event schema ────────────────────────────────────────────────────────────

type streamEvent struct {
	Type string `json:"type"`

	AssistantMessageEvent *assistantMessageEvent `json:"assistantMessageEvent,omitempty"`

	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	IsError    bool            `json:"isError,omitempty"`

	// error: string; turn_end: object.
	Message json.RawMessage `json:"message,omitempty"`

	Success    bool   `json:"success,omitempty"`
	FinalError string `json:"finalError,omitempty"`
}

type assistantMessageEvent struct {
	Type  string `json:"type"`
	Delta string `json:"delta,omitempty"`
}

type message struct {
	Model string `json:"model,omitempty"`
	Usage *usage `json:"usage,omitempty"`
}

type usage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
}

func truncateForLog(b []byte, n int) string {
	s := string(b)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
