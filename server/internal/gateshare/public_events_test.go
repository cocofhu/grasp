package gateshare

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestSanitizeLiveEventsKeepsRailsAndBareToolNames(t *testing.T) {
	ev := SanitizeLiveEvents([]models.AcpEvent{
		{Kind: "thought", Text: "正在改 http://127.0.0.1/api/runs/secret"},
		{Kind: "tool_call", Title: "write_artifact(prd.md)", Text: "should-drop", Status: "completed",
			Artifact: &models.ArtifactMeta{Name: "prd.md", Kind: "md"}},
		{Kind: "message", Text: "标题已改为绿色"},
		{Kind: "plan", Text: "plan-secret"},
		{Kind: "segment"},
	})
	if len(ev) != 3 {
		t.Fatalf("events=%d %+v", len(ev), ev)
	}
	if ev[0].Kind != "thought" || ev[0].Text != "正在改 http://127.0.0.1/api/runs/secret" {
		t.Fatalf("thought must pass through verbatim: %+v", ev[0])
	}
	if ev[1].Kind != "tool_call" || ev[1].Title != "write_artifact" || ev[1].Status != "completed" || ev[1].Text != "" || ev[1].Parts != nil {
		t.Fatalf("tool must keep only its bare name and status: %+v", ev[1])
	}
	if ev[2].Kind != "message" || ev[2].Text != "标题已改为绿色" {
		t.Fatalf("message: %+v", ev[2])
	}
}

func TestSanitizeLiveEventsTimelineDropsToolDetails(t *testing.T) {
	ev := SanitizeLiveEvents([]models.AcpEvent{{Kind: models.AcpKindTimeline, Parts: []models.AcpPart{
		{Kind: "thought", Text: "想一下"},
		{Kind: "tool", Title: "Shell curl http://10.0.0.1", Status: "completed", Summary: "curl http://10.0.0.1", Input: "{}", Output: "secret body"},
		{Kind: "message", Text: "好了"},
		{Kind: "message", Text: "  "},
		{Kind: "weird", Text: "x"},
	}}})
	if len(ev) != 1 || ev[0].Kind != models.AcpKindTimeline {
		t.Fatalf("events: %+v", ev)
	}
	want := []models.AcpPart{{Kind: "thought", Text: "想一下"}, {Kind: "tool", Title: "Shell", Status: "completed"}, {Kind: "message", Text: "好了"}}
	if len(ev[0].Parts) != len(want) {
		t.Fatalf("parts: %+v", ev[0].Parts)
	}
	for i, p := range ev[0].Parts {
		if p != want[i] {
			t.Fatalf("part %d = %+v, want %+v", i, p, want[i])
		}
	}
	if SanitizeLiveEvents([]models.AcpEvent{{Kind: models.AcpKindTimeline}}) != nil {
		t.Fatal("empty timeline must be dropped")
	}
}

func TestPublicAcpFrameKeepsSanitizedTimeline(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"type": "acp", "nodeId": "n1", "events": []any{
		map[string]any{"kind": "timeline", "parts": []any{
			map[string]any{"kind": "tool", "title": "Read /etc/passwd", "status": "completed", "summary": "/etc/passwd", "output": "root:x"},
			"junk",
		}},
	}})
	out, ok := FilterPublicBrokerFrame(raw, "n1", func() int { return 0 })
	if !ok {
		t.Fatal("frame dropped")
	}
	if s := string(out); strings.Contains(s, "passwd") || strings.Contains(s, "root:x") || !strings.Contains(s, `"title":"Read"`) {
		t.Fatalf("frame: %s", s)
	}
}

func TestSanitizeToolTitle(t *testing.T) {
	cases := map[string]string{
		"read_file":                    "read_file",
		"Shell":                        "Shell",
		"mcp:grasp.write-artifact":     "mcp:grasp.write-artifact",
		"write_artifact(prd.md)":       "write_artifact",
		"Read /etc/passwd":             "Read",
		"run_terminal_cmd\ncat ~/.ssh": "run_terminal_cmd",
		"/home/u/.env":                 "tool",
		"https://example.com/x":        "tool",
		"读取文件":                         "tool",
		"":                             "tool",
		strings.Repeat("a", 41):        "tool",
	}
	for in, want := range cases {
		if got := SanitizeToolTitle(in); got != want {
			t.Errorf("SanitizeToolTitle(%q) = %q, want %q", in, got, want)
		}
	}
	if SanitizeToolStatus(" Completed ") != "completed" || SanitizeToolStatus("weird") != "" {
		t.Fatal("status whitelist")
	}
}

func TestSanitizeTurnsReducesAgentTools(t *testing.T) {
	turns := SanitizeTurns([]models.ReactMessage{
		{Role: "agent", Text: "ok", Tools: []models.ReactTool{{Title: "Read src/secret.ts", Status: "completed"}, {Title: "/x", Status: "nope"}}},
		{Role: "human", Text: "hi", Tools: []models.ReactTool{{Title: "Shell"}}},
	})
	if len(turns) != 2 {
		t.Fatalf("turns: %+v", turns)
	}
	want := []models.ReactTool{{Title: "Read", Status: "completed"}, {Title: "tool"}}
	if len(turns[0].Tools) != 2 || turns[0].Tools[0] != want[0] || turns[0].Tools[1] != want[1] {
		t.Fatalf("agent tools: %+v", turns[0].Tools)
	}
	if turns[0].Parts != nil {
		t.Fatalf("no parts expected: %+v", turns[0].Parts)
	}
	if turns[1].Tools != nil {
		t.Fatalf("human turns carry no tools: %+v", turns[1].Tools)
	}
}

func TestSanitizeLiveEventsCapsTools(t *testing.T) {
	var ev []models.AcpEvent
	for i := 0; i < models.MaxReactTools+5; i++ {
		ev = append(ev, models.AcpEvent{Kind: "tool_call", Title: "t"})
	}
	if got := SanitizeLiveEvents(ev); len(got) != models.MaxReactTools {
		t.Fatalf("len=%d", len(got))
	}
}

func base(n int) func() int { return func() int { return n } }

func TestSanitizeLiveEventsKeepsLongThought(t *testing.T) {
	long := strings.Repeat("思", 20000) + "最新一句"
	ev := SanitizeLiveEvents([]models.AcpEvent{{Kind: "thought", Text: long}})
	if len(ev) != 1 || ev[0].Text != long {
		t.Fatalf("thought cut: got %d runes", len([]rune(ev[0].Text)))
	}
}

func TestSanitizeTurnsKeepsTextVerbatim(t *testing.T) {
	long := strings.Repeat("长", 20000) + " http://10.1.2.3/api/x run-0123abcd"
	turns := SanitizeTurns([]models.ReactMessage{{Role: "agent", Text: long}})
	if len(turns) != 1 || turns[0].Text != long {
		t.Fatal("turn text must match the approval page verbatim")
	}
}

func TestFilterPublicBrokerFrameStripsRunAndRewritesNode(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"type":   "acp",
		"runId":  "run-secret",
		"nodeId": "research1",
		"busy":   true,
		"events": []any{
			map[string]any{"kind": "message", "text": "流式正文 http://10.1.2.3/api/x"},
			map[string]any{"kind": "tool_call", "title": "write src/leak.ts", "text": "leak", "status": "running"},
		},
	})
	out, ok := FilterPublicBrokerFrame(raw, "research1", func() int {
		t.Fatal("acp frames must not read the image base")
		return 0
	})
	if !ok {
		t.Fatal("expected filtered frame")
	}
	s := string(out)
	if strings.Contains(s, "run-secret") || strings.Contains(s, "research1") || strings.Contains(s, "leak") {
		t.Fatalf("leaked: %s", s)
	}
	if !strings.Contains(s, `{"kind":"tool_call","title":"write","status":"running"}`) {
		t.Fatalf("tool row must keep its bare name: %s", s)
	}
	if !strings.Contains(s, PublicDialogueNodeID) || !strings.Contains(s, "流式正文") {
		t.Fatalf("missing public payload: %s", s)
	}
	if !strings.Contains(s, "流式正文 http://10.1.2.3/api/x") {
		t.Fatalf("message must pass through verbatim: %s", s)
	}

	other, ok := FilterPublicBrokerFrame(raw, "other-node", base(0))
	if ok || other != nil {
		t.Fatalf("other node must drop: ok=%v %s", ok, other)
	}
}

func TestFilterPublicBrokerFrameReviewTurnBegin(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"type":   "review",
		"runId":  "run-secret",
		"nodeId": "research1",
		"event":  "turn_begin",
		"item": map[string]any{
			"id":     "q1",
			"text":   "改成绿的",
			"images": []any{map[string]any{"data": "AAAA", "mimeType": "image/png", "name": "x.png"}},
		},
	})
	out, ok := FilterPublicBrokerFrame(raw, "research1", base(2))
	if !ok {
		t.Fatal("expected review frame")
	}
	s := string(out)
	if strings.Contains(s, "run-secret") || strings.Contains(s, "AAAA") || strings.Contains(s, "blob:") {
		t.Fatalf("leaked: %s", s)
	}
	if !strings.Contains(s, `"event":"turn_begin"`) || !strings.Contains(s, "改成绿的") {
		t.Fatalf("payload: %s", s)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	item, _ := parsed["item"].(map[string]any)
	imgs, _ := item["images"].([]any)
	if len(imgs) != 1 {
		t.Fatalf("item images: %+v", item)
	}
	im, _ := imgs[0].(map[string]any)
	if im["index"] != float64(2) || im["name"] != "x.png" {
		t.Fatalf("opaque image: %+v", im)
	}
}

func TestFilterPublicBrokerFrameQueueStateKeepsAnnotations(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"type":    "review",
		"runId":   "run-secret",
		"nodeId":  "research1",
		"event":   "queue_state",
		"waiting": 1,
		"busy":    false,
		"items": []any{
			map[string]any{
				"id":   "q-wait",
				"text": "公共排队带标注",
				"annotations": []any{
					map[string]any{"selector": "#pub-hero", "label": "公共点", "jsonPath": "goals[0]"},
				},
				"images": []any{map[string]any{"data": "BBBB", "mimeType": "image/png", "name": "q.png"}},
			},
		},
	})
	out, ok := FilterPublicBrokerFrame(raw, "research1", base(0))
	if !ok {
		t.Fatal("expected queue_state frame")
	}
	s := string(out)
	if strings.Contains(s, "run-secret") || strings.Contains(s, "BBBB") {
		t.Fatalf("leaked: %s", s)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	items, _ := parsed["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items=%+v", parsed["items"])
	}
	row, _ := items[0].(map[string]any)
	anns, _ := row["annotations"].([]any)
	if len(anns) != 1 {
		t.Fatalf("annotations missing: %+v", row)
	}
	ann, _ := anns[0].(map[string]any)
	if ann["selector"] != "#pub-hero" || ann["label"] != "公共点" {
		t.Fatalf("ann fields: %+v", ann)
	}
	imgs, _ := row["images"].([]any)
	if len(imgs) != 1 {
		t.Fatalf("images missing: %+v", row)
	}
	im, _ := imgs[0].(map[string]any)
	if im["index"] != float64(0) || im["name"] != "q.png" {
		t.Fatalf("queue image index: %+v", im)
	}
	if strings.Contains(s, "blob:") || strings.Contains(s, "/api/blobs") {
		t.Fatalf("image path leak: %s", s)
	}
}

func TestFilterPublicBrokerFrameLive(t *testing.T) {
	raw := []byte(`{"type":"live","runId":"r1","nodeId":"p1","session":{"sid":"sid001","runId":"r1","nodeId":"p1","state":"ready","retryAccept":true,"variants":[{"n":1}],"summary":"h1"}}`)
	out, ok := FilterPublicBrokerFrame(raw, "p1", base(0))
	if !ok {
		t.Fatal("live frame dropped")
	}
	s := string(out)
	if strings.Contains(s, `"r1"`) || !strings.Contains(s, `"state":"ready"`) || !strings.Contains(s, `"sid":"sid001"`) {
		t.Fatalf("filtered = %s", s)
	}
	if !strings.Contains(s, `"retryAccept":true`) {
		t.Fatalf("failed acceptance recovery flag stripped from public frame: %s", s)
	}
	if _, ok := FilterPublicBrokerFrame([]byte(`{"type":"live","nodeId":"p1"}`), "p1", base(0)); ok {
		t.Fatal("frame without session must drop")
	}
	if _, ok := FilterPublicBrokerFrame(raw, "other", base(0)); ok {
		t.Fatal("other node must drop")
	}
}

func TestSanitizeTurnsReducesAgentParts(t *testing.T) {
	turns := SanitizeTurns([]models.ReactMessage{
		{Role: "agent", Text: "ok", Parts: []models.AcpPart{
			{Kind: "tool", Title: "Read src/secret.ts", Status: "completed", Summary: "src/secret.ts", Input: "in", Output: "out"},
			{Kind: "message", Text: "ok"},
		}},
		{Role: "human", Text: "hi", Parts: []models.AcpPart{{Kind: "message", Text: "x"}}},
	})
	if len(turns) != 2 || len(turns[0].Parts) != 2 || turns[1].Parts != nil {
		t.Fatalf("turns: %+v", turns)
	}
	if turns[0].Parts[0] != (models.AcpPart{Kind: "tool", Title: "Read", Status: "completed"}) {
		t.Fatalf("tool part: %+v", turns[0].Parts[0])
	}
	var many []models.AcpPart
	for range maxPublicParts + 5 {
		many = append(many, models.AcpPart{Kind: "message", Text: "m"})
	}
	if got := SanitizeParts(many); len(got) != maxPublicParts {
		t.Fatalf("cap: %d", len(got))
	}
	if SanitizeParts([]models.AcpPart{{Kind: "x"}}) != nil {
		t.Fatal("unknown kinds dropped")
	}
}
