package gateshare

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestSanitizeLiveEventsKeepsRailsAndDropsTools(t *testing.T) {
	ev := SanitizeLiveEvents([]models.AcpEvent{
		{Kind: "thought", Text: "正在改 http://127.0.0.1/api/runs/secret"},
		{Kind: "tool_call", Title: "write", Text: "should-drop"},
		{Kind: "message", Text: "标题已改为绿色"},
		{Kind: "plan", Text: "plan-secret"},
	})
	if len(ev) != 2 {
		t.Fatalf("events=%d %+v", len(ev), ev)
	}
	if ev[0].Kind != "thought" || ev[0].Text != "正在改 http://127.0.0.1/api/runs/secret" {
		t.Fatalf("thought must pass through verbatim: %+v", ev[0])
	}
	if ev[1].Kind != "message" || ev[1].Text != "标题已改为绿色" {
		t.Fatalf("message: %+v", ev[1])
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
			map[string]any{"kind": "tool_call", "title": "write", "text": "leak"},
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
	if strings.Contains(s, "run-secret") || strings.Contains(s, "research1") || strings.Contains(s, "tool_call") {
		t.Fatalf("leaked: %s", s)
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
