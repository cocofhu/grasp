package streamjson

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"unicode"

	"backend/internal/provider/oneshot"
)

// maxToolResultText caps the tool output forwarded per call; the server
// redacts and truncates again before anything reaches a browser.
const maxToolResultText = 4000

// cursorToolEvent is cursor-agent's top-level tool event:
//
//	{"type":"tool_call","subtype":"started|completed","call_id":"…",
//	 "tool_call":{"shellToolCall":{"args":{…},"result":{"success":{…}}},
//	              "startedAtMs":"…","completedAtMs":"…"}}
//
// The single *ToolCall key names the tool; MCP calls use mcpToolCall with the
// server tool in args.toolName and its arguments in args.args.
type cursorToolEvent struct {
	Subtype  string                     `json:"subtype"`
	CallID   string                     `json:"call_id"`
	ToolCall map[string]json.RawMessage `json:"tool_call"`
}

type cursorToolBody struct {
	Args       json.RawMessage `json:"args"`
	Result     json.RawMessage `json:"result"`
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments"`
	ToolCallID string          `json:"toolCallId"`
}

// parseCursorTool maps one cursor tool_call line to a tool start or result.
func parseCursorTool(line []byte) []oneshot.Msg {
	var ev cursorToolEvent
	if json.Unmarshal(line, &ev) != nil || len(ev.ToolCall) == 0 {
		return nil
	}
	key, body := cursorToolKey(ev.ToolCall)
	if key == "" {
		return nil
	}
	name, input := cursorToolNameInput(key, body)
	id := ev.CallID
	if id == "" {
		id = body.ToolCallID
	}
	if id == "" {
		id = fallbackToolID(key, body.Args)
	}
	switch ev.Subtype {
	case "started":
		return []oneshot.Msg{{Kind: oneshot.KindToolUse, ToolCallID: id, ToolTitle: name, RawInput: input}}
	case "completed":
		text, failed := cursorToolResult(body.Result)
		m := oneshot.Msg{Kind: oneshot.KindToolResult, ToolCallID: id, Text: text, DurationMs: cursorToolDuration(ev.ToolCall)}
		if failed {
			m.ToolStatus = "failed"
		}
		if key == "shellToolCall" {
			m.BackgroundPID = cursorBackgroundPID(body.Result)
		}
		return []oneshot.Msg{m}
	}
	return nil
}

// cursorToolKey returns the *ToolCall (or "function") entry; sibling keys such
// as startedAtMs / toolCallId describe the call, not the tool.
func cursorToolKey(call map[string]json.RawMessage) (string, cursorToolBody) {
	for k, raw := range call {
		if k != "function" && !strings.HasSuffix(k, "ToolCall") {
			continue
		}
		var body cursorToolBody
		if json.Unmarshal(raw, &body) != nil {
			continue
		}
		return k, body
	}
	return "", cursorToolBody{}
}

func cursorToolNameInput(key string, body cursorToolBody) (string, json.RawMessage) {
	switch key {
	case "mcpToolCall":
		var a struct {
			ToolName string          `json:"toolName"`
			Name     string          `json:"name"`
			Args     json.RawMessage `json:"args"`
		}
		_ = json.Unmarshal(body.Args, &a)
		name := a.ToolName
		if name == "" {
			name = a.Name
		}
		if name == "" {
			name = "mcp"
		}
		return name, a.Args
	case "function":
		name := body.Name
		if name == "" {
			name = "function"
		}
		return name, functionArguments(body.Arguments)
	}
	return toolDisplayName(strings.TrimSuffix(key, "ToolCall")), trimArgs(body.Args)
}

// cursorArgNoise are bookkeeping fields cursor adds to tool args (shell parse
// trees, approval flags); they would bury the command or path in the detail.
var cursorArgNoise = map[string]bool{
	"toolCallId": true, "parsingResult": true, "simpleCommands": true, "hasInputRedirect": true,
	"hasOutputRedirect": true, "fileOutputThresholdBytes": true, "skipApproval": true,
	"smartModeApprovalOnly": true, "isBackground": true, "requestedSandboxPolicy": true,
}

func trimArgs(raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	for k, v := range m {
		if cursorArgNoise[k] || string(v) == `""` || string(v) == "null" {
			delete(m, k)
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return b
}

// functionArguments accepts arguments as an object or as a JSON-encoded string.
func functionArguments(raw json.RawMessage) json.RawMessage {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if json.Valid([]byte(s)) {
			return json.RawMessage(s)
		}
		b, _ := json.Marshal(map[string]string{"arguments": s})
		return b
	}
	return raw
}

// toolDisplayName turns cursor's camelCase tool kind into a title: shell →
// Shell, readLints → Read lints.
func toolDisplayName(kind string) string {
	if kind == "" {
		return "Tool"
	}
	var b strings.Builder
	for i, r := range kind {
		switch {
		case i == 0:
			b.WriteRune(unicode.ToUpper(r))
		case unicode.IsUpper(r):
			b.WriteByte(' ')
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// cursorToolResult extracts readable output and whether the call failed.
func cursorToolResult(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return clipText(s), s == "rejected"
	}
	var r map[string]json.RawMessage
	if json.Unmarshal(raw, &r) != nil {
		return "", false
	}
	for _, k := range []string{"error", "failure", "rejected"} {
		if v, ok := r[k]; ok {
			t := resultText(v)
			if t == "" {
				t = k
			}
			return clipText(t), true
		}
	}
	if v, ok := r["success"]; ok {
		return clipText(resultText(v)), false
	}
	return clipText(resultText(raw)), false
}

// resultText prefers the human-readable field of a result payload, falling
// back to the compact JSON.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) == nil {
		var parts []string
		for _, k := range []string{"stdout", "stderr", "output", "content", "message", "markdown", "text", "error"} {
			if v, ok := m[k]; ok {
				if t := flatText(v); strings.TrimSpace(t) != "" {
					parts = append(parts, t)
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	return string(raw)
}

// flatText reads a string, or the text of MCP content blocks
// ([{"text":"…"}] or [{"text":{"text":"…"}}]).
func flatText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Text json.RawMessage `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			var t string
			if json.Unmarshal(b.Text, &t) == nil {
				parts = append(parts, t)
				continue
			}
			var inner struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(b.Text, &inner) == nil && inner.Text != "" {
				parts = append(parts, inner.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func clipText(s string) string {
	if len(s) <= maxToolResultText {
		return s
	}
	cut := maxToolResultText
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// cursorBackgroundPID reads the shell pid from a shell result that only
// launched a background task: {"isBackground":true,"success":{"pid":42}}.
func cursorBackgroundPID(raw json.RawMessage) int {
	var r struct {
		IsBackground bool `json:"isBackground"`
		Success      struct {
			PID int `json:"pid"`
		} `json:"success"`
	}
	if json.Unmarshal(raw, &r) != nil || !r.IsBackground || r.Success.PID <= 0 {
		return 0
	}
	return r.Success.PID
}

// cursorToolDuration reads startedAtMs / completedAtMs (sent as strings).
func cursorToolDuration(call map[string]json.RawMessage) int64 {
	start, end := msField(call["startedAtMs"]), msField(call["completedAtMs"])
	if start <= 0 || end < start {
		return 0
	}
	return end - start
}

func msField(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		n, _ := strconv.ParseInt(s, 10, 64)
		return n
	}
	var n int64
	_ = json.Unmarshal(raw, &n)
	return n
}

// fallbackToolID pairs started/completed when cursor omits call_id: both
// carry the same kind and args.
func fallbackToolID(key string, args json.RawMessage) string {
	sum := sha1.Sum(append([]byte(key+"\x00"), args...))
	return "cursor-" + hex.EncodeToString(sum[:6])
}
