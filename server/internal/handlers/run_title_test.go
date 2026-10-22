package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestStartRunAcceptsTitleOverride(t *testing.T) {
	h := newHarness(t)
	reposJSON := `[{"name":"approving","url":"https://git.example/approving.git"}]`
	if err := h.h.WF.Save(&models.WorkflowDef{
		ID: "wf-title", Name: "WF Title",
		Graph: models.Graph{
			Variables: []models.Variable{{Name: "repos", Type: "repos", Ask: true, Value: reposJSON}},
			Nodes:     []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}},
			Edges:     []models.Edge{{ID: "e1", Source: "in", Target: "out"}},
		},
	}); err != nil {
		t.Fatalf("save workflow: %v", err)
	}

	w := h.do(http.MethodPost, "/api/workflows/wf-title/runs", map[string]any{
		"trigger": "manual",
		"title":   "  用户第一句话  ",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var res map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	run, ok := h.h.Runs.Get(res["id"])
	if !ok {
		t.Fatal("run missing")
	}
	if run.Title != "用户第一句话" {
		t.Fatalf("title=%q want 用户第一句话", run.Title)
	}
}

// The home chat sends its opening message with startRun; the engine delivers it
// once the approve node parks, so it must survive on the run row.
func TestStartRunPersistsFirstMessage(t *testing.T) {
	h := newHarness(t)
	if err := h.h.WF.Save(&models.WorkflowDef{
		ID: "wf-first", Name: "WF First",
		Graph: models.Graph{
			Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}},
			Edges: []models.Edge{{ID: "e1", Source: "in", Target: "out"}},
		},
	}); err != nil {
		t.Fatalf("save workflow: %v", err)
	}

	w := h.do(http.MethodPost, "/api/workflows/wf-first/runs", map[string]any{
		"trigger": "manual",
		"firstMessage": map[string]any{
			"text":   "  把登录做清楚  ",
			"images": []any{},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var res map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	var run models.Run
	if err := h.db.First(&run, "id = ?", res["id"]).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.FirstMessage == nil || run.FirstMessage.Text != "把登录做清楚" {
		t.Fatalf("firstMessage=%+v", run.FirstMessage)
	}
	if run.FirstMessageDeliveredAt != nil {
		t.Fatal("delivery latch must start unset")
	}
}

// A blank opening message must not be persisted at all.
func TestStartRunIgnoresBlankFirstMessage(t *testing.T) {
	h := newHarness(t)
	if err := h.h.WF.Save(&models.WorkflowDef{
		ID: "wf-blank", Name: "WF Blank",
		Graph: models.Graph{
			Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}},
			Edges: []models.Edge{{ID: "e1", Source: "in", Target: "out"}},
		},
	}); err != nil {
		t.Fatalf("save workflow: %v", err)
	}

	w := h.do(http.MethodPost, "/api/workflows/wf-blank/runs", map[string]any{
		"trigger":      "manual",
		"firstMessage": map[string]any{"text": "   "},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var res map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	var run models.Run
	if err := h.db.First(&run, "id = ?", res["id"]).Error; err != nil {
		t.Fatalf("load run: %v", err)
	}
	if run.FirstMessage != nil {
		t.Fatalf("blank first message must not persist: %+v", run.FirstMessage)
	}
}

func TestUpdateRunTitleHTTP(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	graph := minimalGraph()
	seed := func(id, status, title string) {
		t.Helper()
		if err := h.db.Create(&models.Run{
			ID: id, Status: status, Title: title, StartedAt: now, Graph: graph,
		}).Error; err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
	seed("rt-live", "running", "原标题")
	seed("rt-done", "completed", "已结束")
	seed("rt-other", "running", "另一条")

	titleOf := func(id string) string {
		t.Helper()
		got, ok := h.h.Runs.Get(id)
		if !ok {
			t.Fatalf("missing run %s", id)
		}
		return got.Title
	}

	t.Run("writes a trimmed title and reads it back", func(t *testing.T) {
		w := h.do(http.MethodPatch, "/api/runs/rt-live/title", map[string]string{"title": "  把登录做清楚  "})
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["id"] != "rt-live" || body["title"] != "把登录做清楚" {
			t.Fatalf("body=%s", w.Body.String())
		}
		if titleOf("rt-live") != "把登录做清楚" {
			t.Fatalf("stored=%q", titleOf("rt-live"))
		}

		exact := strings.Repeat("字", 80)
		w = h.do(http.MethodPatch, "/api/runs/rt-live/title", map[string]string{"title": " " + exact + " "})
		if w.Code != http.StatusOK {
			t.Fatalf("80 status=%d body=%s", w.Code, w.Body.String())
		}
		if titleOf("rt-live") != exact {
			t.Fatalf("80 stored runes=%d", len([]rune(titleOf("rt-live"))))
		}
		list := h.do(http.MethodGet, "/api/runs", nil)
		if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), exact) {
			t.Fatalf("list missing title: %d %s", list.Code, list.Body.String())
		}
	})

	t.Run("rejects blank without changing the stored title", func(t *testing.T) {
		before := titleOf("rt-live")
		w := h.do(http.MethodPatch, "/api/runs/rt-live/title", map[string]string{"title": " \n\t "})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if titleOf("rt-live") != before {
			t.Fatalf("blank write changed title to %q", titleOf("rt-live"))
		}
	})

	t.Run("rejects the 81st character without truncating", func(t *testing.T) {
		before := titleOf("rt-other")
		tooLong := strings.Repeat("字", 81)
		w := h.do(http.MethodPatch, "/api/runs/rt-other/title", map[string]string{"title": tooLong})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if titleOf("rt-other") != before {
			t.Fatalf("overlong write changed title to %q", titleOf("rt-other"))
		}
	})

	t.Run("missing run", func(t *testing.T) {
		w := h.do(http.MethodPatch, "/api/runs/missing/title", map[string]string{"title": "不存在"})
		if w.Code != http.StatusNotFound {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("terminal run can still be renamed", func(t *testing.T) {
		w := h.do(http.MethodPatch, "/api/runs/rt-done/title", map[string]string{"title": "结束后的名字"})
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if titleOf("rt-done") != "结束后的名字" {
			t.Fatalf("stored=%q", titleOf("rt-done"))
		}
	})

	t.Run("duplicate names are allowed", func(t *testing.T) {
		for _, id := range []string{"rt-done", "rt-other"} {
			w := h.do(http.MethodPatch, "/api/runs/"+id+"/title", map[string]string{"title": "同名"})
			if w.Code != http.StatusOK {
				t.Fatalf("%s status=%d body=%s", id, w.Code, w.Body.String())
			}
		}
		if titleOf("rt-done") != "同名" || titleOf("rt-other") != "同名" {
			t.Fatalf("done=%q other=%q", titleOf("rt-done"), titleOf("rt-other"))
		}
	})
}

// GetRun must emit the same run-level title as ListRuns so the conversation
// toolbar can match the sidebar row. Nested gate.title is a different field.
func TestGetRunExposesTitleMatchingList(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	if err := h.db.Create(&models.Run{
		ID: "rd-title", WorkflowID: "wf-self", WorkflowName: "自我迭代",
		Title: "这个行高有点高了", Status: "running", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}}},
	}).Error; err != nil {
		t.Fatalf("seed run: %v", err)
	}

	detail := h.do(http.MethodGet, "/api/runs/rd-title", nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("get run: %d %s", detail.Code, detail.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(detail.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["title"] != "这个行高有点高了" {
		t.Fatalf("detail title=%v want 这个行高有点高了", got["title"])
	}
	if got["workflowName"] != "自我迭代" {
		t.Fatalf("workflowName=%v", got["workflowName"])
	}

	list := h.do(http.MethodGet, "/api/runs", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list: %d %s", list.Code, list.Body)
	}
	var items []map[string]any
	if err := decodePageItems(list.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	var listed map[string]any
	for _, it := range items {
		if it["id"] == "rd-title" {
			listed = it
			break
		}
	}
	if listed == nil {
		t.Fatal("list missing rd-title")
	}
	if listed["title"] != got["title"] {
		t.Fatalf("list title=%v detail title=%v", listed["title"], got["title"])
	}
}

func TestGetRunTitleDoesNotCollideWithGateTitle(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	if err := h.db.Create(&models.Run{
		ID: "rd-gate-title", WorkflowID: "wf-self", WorkflowName: "自我迭代",
		Title: "这个行高有点高了", Status: "waiting_human", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{
			{ID: "impl", Type: "agent", Caps: testAutoCaps},
			{ID: "gate", Type: "human_gate"},
		}},
	}).Error; err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if err := h.db.Create(&models.StateRun{
		RunID: "rd-gate-title", NodeID: "gate", Iteration: 1, Status: "waiting_human",
	}).Error; err != nil {
		t.Fatalf("seed state: %v", err)
	}
	if err := h.db.Create(&models.Gate{
		RunID: "rd-gate-title", NodeID: "gate", Iteration: 1, Title: "审批", RequestedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed gate: %v", err)
	}

	w := h.do(http.MethodGet, "/api/runs/rd-gate-title", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get run: %d %s", w.Code, w.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["title"] != "这个行高有点高了" {
		t.Fatalf("top-level title=%v (must stay the run title)", got["title"])
	}
	gate, _ := got["gate"].(map[string]any)
	if gate == nil {
		t.Fatal("expected nested gate")
	}
	if gate["title"] != "审批" {
		t.Fatalf("gate.title=%v want 审批", gate["title"])
	}

	if err := h.db.Create(&models.Run{
		ID: "rd-empty-title", WorkflowName: "自我迭代", Title: "", Status: "running", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{{ID: "in", Type: "input"}}},
	}).Error; err != nil {
		t.Fatalf("seed empty: %v", err)
	}
	empty := h.do(http.MethodGet, "/api/runs/rd-empty-title", nil)
	if empty.Code != http.StatusOK {
		t.Fatalf("empty get: %d %s", empty.Code, empty.Body)
	}
	var emptyGot map[string]any
	if err := json.Unmarshal(empty.Body.Bytes(), &emptyGot); err != nil {
		t.Fatal(err)
	}
	if title, ok := emptyGot["title"].(string); !ok || title != "" {
		t.Fatalf("empty title must be present as \"\", got %v (%T)", emptyGot["title"], emptyGot["title"])
	}
}
