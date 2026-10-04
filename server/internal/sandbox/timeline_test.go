package sandbox

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/models"
)

func msgChunk(r *ChatResult, s string) {
	dispatchSessionUpdate("agent_message_chunk", map[string]any{"content": map[string]any{"text": s}}, r)
}

func thoughtChunk(r *ChatResult, s string) {
	dispatchSessionUpdate("agent_thought_chunk", map[string]any{"content": s}, r)
}

func timelineOf(t *testing.T, r *ChatResult) []models.AcpPart {
	t.Helper()
	ev := r.AcpEvents()
	if len(ev) == 0 || ev[len(ev)-1].Kind != models.AcpKindTimeline {
		t.Fatalf("last event is not a timeline: %+v", ev)
	}
	return ev[len(ev)-1].Parts
}

func TestTimelineKeepsArrivalOrder(t *testing.T) {
	r := &ChatResult{}
	thoughtChunk(r, "先看")
	thoughtChunk(r, "仓库")
	dispatchSessionUpdate("tool_call", map[string]any{"toolCallId": "t1", "title": "Shell", "status": "in_progress",
		"rawInput": map[string]any{"command": "ls   -la\n/src"}}, r)
	msgChunk(r, "看到了")
	msgChunk(r, "。")
	dispatchSessionUpdate("tool_call", map[string]any{"toolCallId": "t2", "title": "Read", "rawInput": map[string]any{"path": "a.go"}}, r)
	dispatchSessionUpdate("tool_call_update", map[string]any{"toolCallId": "t1", "status": "completed",
		"rawOutput": map[string]any{"stdout": "file.txt", "stderr": "warn"}}, r)
	thoughtChunk(r, "再想想")
	msgChunk(r, "完成")

	parts := timelineOf(t, r)
	kinds := make([]string, len(parts))
	for i, p := range parts {
		kinds[i] = p.Kind
	}
	if got := strings.Join(kinds, ","); got != "thought,tool,message,tool,thought,message" {
		t.Fatalf("order = %s", got)
	}
	if parts[0].Text != "先看仓库" || parts[2].Text != "看到了。" || parts[5].Text != "完成" {
		t.Fatalf("text runs: %+v", parts)
	}
	if parts[1].Title != "Shell" || parts[1].Status != "completed" || parts[1].Summary != "ls -la /src" || parts[1].Output != "file.txt\nwarn" {
		t.Fatalf("tool update must land on the first step: %+v", parts[1])
	}
	if parts[3].Summary != "a.go" || !strings.Contains(parts[3].Input, `"path": "a.go"`) {
		t.Fatalf("second tool: %+v", parts[3])
	}
	// The flat rails stay as before for older consumers.
	if r.Narration != "看到了。完成" || r.Thought != "先看仓库再想想" || len(r.ToolCalls) != 2 {
		t.Fatalf("flat rails changed: %q %q %d", r.Narration, r.Thought, len(r.ToolCalls))
	}
}

func TestTimelineResetsOnSegmentAndSkipsPlanTools(t *testing.T) {
	r := &ChatResult{}
	msgChunk(r, "one")
	r.sealSegment()
	if r.timelineEvent(0) != nil {
		t.Fatal("sealed segment must not stay on the open row")
	}
	dispatchSessionUpdate("tool_call", map[string]any{"toolCallId": "p", "title": "update_todos",
		"rawInput": map[string]any{"todos": []any{map[string]any{"content": "x"}}}}, r)
	msgChunk(r, "  ")
	if r.timelineEvent(0) != nil {
		t.Fatal("plan tools and blank text are not steps")
	}
	r.Timeline = append(r.Timeline, ChatStep{Kind: "tool", Tool: 9})
	if r.timelineEvent(0) != nil {
		t.Fatal("dangling tool index is skipped")
	}
}

func TestTimelineCaps(t *testing.T) {
	r := &ChatResult{}
	for i := range MaxTimelineSteps + 10 {
		if i%2 == 0 {
			thoughtChunk(r, "t")
		} else {
			msgChunk(r, "m")
		}
	}
	if n := len(timelineOf(t, r)); n != MaxTimelineSteps {
		t.Fatalf("steps = %d", n)
	}

	// plan coverage: g1.1 / g2.1 — thought and message over 8000 bytes stay full.
	longMsg := strings.Repeat("回", maxStepText) + "结尾标记END"
	if len(longMsg) <= maxStepText {
		t.Fatalf("message fixture too short: %d", len(longMsg))
	}
	big := &ChatResult{}
	msgChunk(big, longMsg)
	if p := timelineOf(t, big); p[0].Kind != "message" || p[0].Text != longMsg || !utf8.ValidString(p[0].Text) {
		t.Fatalf("long message step must equal original (%d bytes), got %d", len(longMsg), len(p[0].Text))
	} else if strings.Contains(p[0].Text, "…(truncated)") || strings.Contains(p[0].Text, "...(truncated)") {
		t.Fatal("long message step must not be truncated")
	}
	exact := strings.Repeat("a", maxStepText)
	exactRow := &ChatResult{}
	msgChunk(exactRow, exact)
	if p := timelineOf(t, exactRow); p[0].Text != exact {
		t.Fatalf("exactly-%d message must be unchanged", maxStepText)
	}

	longThought := strings.Repeat("思", maxStepText) + "思考结尾END"
	bigThought := &ChatResult{}
	thoughtChunk(bigThought, longThought)
	if p := timelineOf(t, bigThought); p[0].Kind != "thought" || p[0].Text != longThought || !utf8.ValidString(p[0].Text) {
		t.Fatalf("long thought step must equal original")
	} else if strings.Contains(p[0].Text, "…(truncated)") || strings.Contains(p[0].Text, "...(truncated)") {
		t.Fatal("long thought step must not be truncated")
	}

	// Combined prose over the 64KiB budget stays full (plan g2.1).
	block := strings.Repeat("文", 12000)
	if len(block)*2 <= maxTimelineText {
		t.Fatalf("prose fixture must exceed budget: %d", len(block)*2)
	}
	over := &ChatResult{}
	thoughtChunk(over, block)
	msgChunk(over, block)
	overParts := timelineOf(t, over)
	if len(overParts) != 2 || overParts[0].Text != block || overParts[1].Text != block {
		t.Fatalf("prose over 64KiB must stay full: %+v", overParts)
	}

	// Tool details still drop once prose plus details exhaust the budget.
	budget := &ChatResult{}
	chunk := strings.Repeat("b", maxStepText)
	for range 10 {
		msgChunk(budget, chunk)
		dispatchSessionUpdate("tool_call", map[string]any{"title": "Read", "rawInput": map[string]any{"path": strings.Repeat("p", 3000)}}, budget)
	}
	prose := 0
	var lastTool models.AcpPart
	for _, p := range timelineOf(t, budget) {
		if p.Kind == "message" {
			if p.Text != chunk || strings.Contains(p.Text, "truncated") {
				t.Fatalf("prose over budget must stay full, got %d bytes", len(p.Text))
			}
			prose += len(p.Text)
		}
		if p.Kind == "tool" {
			lastTool = p
		}
	}
	if prose <= maxTimelineText {
		t.Fatalf("fixture must exceed timeline budget, prose=%d", prose)
	}
	if lastTool.Input != "" || lastTool.Output != "" || lastTool.Summary == "" {
		t.Fatalf("over budget a tool keeps only its summary: %+v", lastTool)
	}
}

func TestToolSummary(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{``, ""},
		{`not json`, ""},
		{`{"command":["bash","-lc","echo hi"]}`, "bash -lc echo hi"},
		{`{"file_path":"src/a.ts","path":"ignored"}`, "src/a.ts"},
		{`{"command":"  ","url":"https://x.dev"}`, "https://x.dev"},
		{`{"command":"curl -H 'Authorization: ` + "Bear" + `er abcdefghijklmnopqrstuvwxyz'"}`, "curl -H 'Authorization: ****'"},
		{`{"other":1}`, ""},
	}
	for _, c := range cases {
		if got := ToolSummary(json.RawMessage(c.in)); got != c.want {
			t.Errorf("ToolSummary(%s) = %q, want %q", c.in, got, c.want)
		}
	}
	long := ToolSummary(json.RawMessage(`{"command":"` + strings.Repeat("中", 300) + `"}`))
	if n := len([]rune(long)); n != maxToolSummary || !strings.HasSuffix(long, "…") {
		t.Fatalf("long summary: %d runes", n)
	}
}

func TestToolDetail(t *testing.T) {
	in := toolDetail(json.RawMessage(`{"path":"a","api_key":"k","nested":{"Password":"p","ok":"token=abcdefghij"}}`), false)
	if strings.Contains(in, `"k"`) || strings.Contains(in, `"p"`) || strings.Contains(in, "abcdefghij") || !strings.Contains(in, `"path": "a"`) {
		t.Fatalf("input not redacted: %s", in)
	}
	cases := map[string]string{
		`"plain out"`: "plain out",
		`[{"type":"text","text":"x"},{"text":"y"}]`: "xy",
		`{"output":"o"}`:             "o",
		`{"stdout":"","stderr":"e"}`: "e",
		`{"content":[{"text":"c"}]}`: "c",
		`{}`:                         "",
		`null`:                       "",
		`bad`:                        "",
		`{"exitCode":0}`:             "{\n  \"exitCode\": 0\n}",
	}
	for raw, want := range cases {
		if got := toolDetail(json.RawMessage(raw), true); got != want {
			t.Errorf("toolDetail(%s) = %q, want %q", raw, got, want)
		}
	}
	if toolDetail(nil, true) != "" {
		t.Fatal("empty raw")
	}
	long := toolDetail(json.RawMessage(`"`+strings.Repeat("x", maxToolDetail+50)+`"`), true)
	if !strings.HasSuffix(long, "…(truncated)") {
		t.Fatal("long detail must be truncated")
	}
	deep := map[string]any{}
	cur := deep
	for range 12 {
		next := map[string]any{}
		cur["n"] = next
		cur = next
	}
	buf, _ := json.Marshal(deep)
	if !strings.Contains(toolDetail(buf, false), "…") {
		t.Fatal("deep input must be cut")
	}
}

func TestTimelineToolDurationAndPlanUpdates(t *testing.T) {
	r := &ChatResult{}
	dispatchSessionUpdate("tool_call", map[string]any{"toolCallId": "p", "title": "Update todos",
		"rawInput": map[string]any{"todos": []any{map[string]any{"content": "x"}}}}, r)
	dispatchSessionUpdate("tool_call_update", map[string]any{"toolCallId": "p", "status": "completed"}, r)
	dispatchSessionUpdate("tool_call", map[string]any{"toolCallId": "s", "title": "Shell", "rawInput": map[string]any{"command": "npm ci"}}, r)
	dispatchSessionUpdate("tool_call_update", map[string]any{"toolCallId": "s", "status": "completed", "durationMs": float64(42000)}, r)
	dispatchSessionUpdate("tool_call", map[string]any{"toolCallId": "n", "title": "Read", "duration_ms": "1500"}, r)
	dispatchSessionUpdate("tool_call", map[string]any{"toolCallId": "z", "title": "Grep", "durationMs": "soon"}, r)

	if len(r.ToolCalls) != 3 {
		t.Fatalf("plan tool updates must stay folded into the plan: %+v", r.ToolCalls)
	}
	parts := timelineOf(t, r)
	if len(parts) != 3 || parts[0].Title != "Shell" || parts[0].DurationMs != 42000 || parts[1].DurationMs != 1500 || parts[2].DurationMs != 0 {
		t.Fatalf("durations: %+v", parts)
	}
}
