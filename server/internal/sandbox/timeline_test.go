package sandbox

import (
	"encoding/json"
	"strings"
	"testing"

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

	big := &ChatResult{}
	msgChunk(big, strings.Repeat("a", maxStepText+100))
	if p := timelineOf(t, big); !strings.HasSuffix(p[0].Text, "…(truncated)") {
		t.Fatalf("long step must be truncated, got %d bytes", len(p[0].Text))
	}

	budget := &ChatResult{}
	for range 10 {
		msgChunk(budget, strings.Repeat("b", maxStepText))
		dispatchSessionUpdate("tool_call", map[string]any{"title": "Read", "rawInput": map[string]any{"path": strings.Repeat("p", 3000)}}, budget)
	}
	total := 0
	var lastTool models.AcpPart
	for _, p := range timelineOf(t, budget) {
		total += len(p.Text) + len(p.Input) + len(p.Output)
		if p.Kind == "tool" {
			lastTool = p
		}
	}
	if total > maxTimelineText+200 {
		t.Fatalf("timeline text %d exceeds budget", total)
	}
	if lastTool.Input != "" || lastTool.Summary == "" {
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
