package handlers_test

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/models"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read fail") }

func TestListWorkflowsReturnsDTOs(t *testing.T) {
	h := newHarness(t)
	seedPublishedWorkflow(t, h, "wf-list")
	w := h.do(http.MethodGet, "/api/workflows", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list workflows: %d %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"wf-list"`)) {
		t.Fatalf("list body: %s", w.Body.String())
	}
}

func TestSaveWorkflowValidationAndConflict(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodPost, "/api/workflows", map[string]any{
		"name": "FirstSave",
		"nodes": []map[string]any{
			{"id": "in", "type": "input", "label": "S"},
			{"id": "out", "type": "output", "label": "E"},
		},
		"edges": []map[string]any{{"id": "e", "source": "in", "target": "out"}},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("first save: %d %s", w.Code, w.Body.String())
	}

	body := map[string]any{
		"name": "DupName",
		"nodes": []map[string]any{
			{"id": "in", "type": "input", "label": "S"},
			{"id": "out", "type": "output", "label": "E"},
		},
		"edges": []map[string]any{{"id": "e", "source": "in", "target": "out"}},
	}
	if w := h.do(http.MethodPost, "/api/workflows", body); w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	body["id"] = "wf-dup-2"
	if w := h.do(http.MethodPost, "/api/workflows", body); w.Code != http.StatusConflict {
		t.Fatalf("duplicate name: %d %s", w.Code, w.Body.String())
	}
}

func TestRestoreWorkflowVersionHTTP(t *testing.T) {
	h := newHarness(t)
	seedPublishedWorkflow(t, h, "wf-restore")
	w := h.do(http.MethodPost, "/api/workflows/wf-restore/versions/1/restore", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("restore v1: %d %s", w.Code, w.Body.String())
	}
	if w := h.do(http.MethodPost, "/api/workflows/wf-restore/versions/bad/restore", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("bad version: %d", w.Code)
	}
	if w := h.do(http.MethodPost, "/api/workflows/ghost/versions/1/restore", nil); w.Code != http.StatusNotFound {
		t.Fatalf("missing wf restore: %d", w.Code)
	}
}

func TestListGatePrimaryArtifactsEmptyItems(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{
		ID: "run-gate-empty", Status: "waiting_human", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{{ID: "gate", Type: "human_gate"}}},
	})
	h.db.Create(&models.Gate{RunID: "run-gate-empty", NodeID: "gate", Iteration: 1, RequestedAt: now})
	w := h.do(http.MethodGet, "/api/runs/run-gate-empty/nodes/gate/primary-artifacts", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list primary: %d %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"items":[]`)) {
		t.Fatalf("body: %s", w.Body.String())
	}
}

func TestReactReplyBadBody(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodPost, "/api/runs/r1/nodes/n1/reply", "bad")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("react bad body: %d", w.Code)
	}
}

func TestStartRunInvalidJSON(t *testing.T) {
	h := newHarness(t)
	seedPublishedWorkflow(t, h, "wf-run-json")
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/wf-run-json/runs", bytes.NewReader([]byte(`{`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: h.cookie})
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid start run json: %d %s", w.Code, w.Body.String())
	}
}

func TestUpdateRunPriorityHTTP(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{ID: "rp-live", Status: "running", StartedAt: now, Graph: minimalGraph()})
	h.db.Create(&models.Run{ID: "rp-done", Status: "completed", StartedAt: now, Graph: minimalGraph()})

	w := h.do(http.MethodPatch, "/api/runs/rp-live/priority", map[string]string{"priority": "high"})
	if w.Code != http.StatusOK {
		t.Fatalf("update priority: %d %s", w.Code, w.Body.String())
	}
	if w := h.do(http.MethodPatch, "/api/runs/ghost/priority", map[string]string{"priority": "high"}); w.Code != http.StatusNotFound {
		t.Fatalf("missing run: %d", w.Code)
	}
	if w := h.do(http.MethodPatch, "/api/runs/rp-live/priority", "bad"); w.Code != http.StatusBadRequest {
		t.Fatalf("bad body: %d", w.Code)
	}
	if w := h.do(http.MethodPatch, "/api/runs/rp-done/priority", map[string]string{"priority": "high"}); w.Code != http.StatusBadRequest {
		t.Fatalf("terminal run: %d %s", w.Code, w.Body.String())
	}
}

func TestMCPRPCNotificationAndReadError(t *testing.T) {
	h := newHarness(t)
	tok := h.host.RegisterRun("run-mcp-gap")

	req := httptest.NewRequest(http.MethodPost, "/mcp/runs/run-mcp-gap",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)))
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("notification ack: %d", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("notification should have empty body, got %q", w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp/runs/run-mcp-gap",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)))
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	sid := w.Header().Get("Mcp-Session-Id")
	if w.Code != http.StatusOK || sid == "" {
		t.Fatalf("initialize session: %d %s", w.Code, w.Body)
	}

	req = httptest.NewRequest(http.MethodDelete, "/mcp/runs/run-mcp-gap", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Mcp-Session-Id", sid)
	w = httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete ack: %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp/runs/run-mcp-gap", errReader{})
	req.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("read body error: %d %s", w.Code, w.Body.String())
	}
}

func TestImportWorkflowReadError(t *testing.T) {
	h := newHarness(t)
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/import", errReader{})
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: h.cookie})
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("import read error: %d %s", w.Code, w.Body.String())
	}
}

func TestNodeEventsEmptyNonPaginated(t *testing.T) {
	h := newHarness(t)
	h.db.Create(&models.Run{ID: "run-ev-empty", Status: "running", StartedAt: time.Now()})
	w := h.do(http.MethodGet, "/api/runs/run-ev-empty/nodes/n1/events", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("events: %d %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"events":[]`)) {
		t.Fatalf("body: %s", w.Body.String())
	}
}

func TestResumeGateBadBody(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodPost, "/api/runs/r1/nodes/g1/decision", "bad")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("resume gate bad body: %d", w.Code)
	}
}

func TestSaveGateArtifactBadBody(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodPut, "/api/runs/r1/nodes/g1/artifacts/x.json", "bad")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("save gate artifact bad body: %d", w.Code)
	}
}

func TestImportAgentValidationBranches(t *testing.T) {
	h := newHarness(t)
	req := httptest.NewRequest(http.MethodPost, "/api/agents/import", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: h.cookie})
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing file: %d", w.Code)
	}

	var buf bytes.Buffer
	req = httptest.NewRequest(http.MethodPost, "/api/agents/import", &buf)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: h.cookie})
	w = httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing targetName: %d %s", w.Code, w.Body.String())
	}
}

func TestCopyWorkflowInvalidBody(t *testing.T) {
	h := newHarness(t)
	seedPublishedWorkflow(t, h, "wf-copy-body")
	req := httptest.NewRequest(http.MethodPost, "/api/workflows/wf-copy-body/copy", bytes.NewReader([]byte(`{`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: h.cookie})
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("copy invalid body: %d", w.Code)
	}
}

func TestListGatesBareAndArtifactsPaged(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{ID: "run-gate-bare", Status: "waiting_human", WorkflowID: "wf-1", StartedAt: now})
	h.db.Create(&models.Gate{RunID: "run-gate-bare", NodeID: "gate", Title: "G", RequestedAt: now})
	h.db.Create(&models.Artifact{ID: "art-page", RunID: "run-gate-bare", Name: "a.md", Kind: "markdown", CreatedAt: now})

	if w := h.do(http.MethodGet, "/api/pending-nodes", nil); w.Code != http.StatusOK {
		t.Fatalf("bare gates: %d %s", w.Code, w.Body.String())
	}
	if w := h.do(http.MethodGet, "/api/artifacts?page=1&pageSize=5", nil); w.Code != http.StatusOK {
		t.Fatalf("paged artifacts: %d %s", w.Code, w.Body.String())
	}
}

func TestImportAgentBadZip(t *testing.T) {
	h := newHarness(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("targetName", "BadZipAgent")
	_ = mw.WriteField("mode", "create")
	fw, _ := mw.CreateFormFile("file", "agent.zip")
	_, _ = fw.Write([]byte("not-a-zip"))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/agents/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: h.cookie})
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad zip import: %d %s", w.Code, w.Body.String())
	}
}

func TestSaveWorkflowEmptyName(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodPost, "/api/workflows", map[string]any{
		"name": "   ",
		"nodes": []map[string]any{
			{"id": "in", "type": "input", "label": "S"},
			{"id": "out", "type": "output", "label": "E"},
		},
		"edges": []map[string]any{{"id": "e", "source": "in", "target": "out"}},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty name: %d %s", w.Code, w.Body.String())
	}
}

func TestResumeGateSuccess(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	runID := "run-gate-resume"
	h.db.Create(&models.Run{
		ID: runID, Status: "waiting_human", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{
			{ID: "upstream", Type: "input"},
			{ID: "gate", Type: "human_gate", Config: map[string]any{
				"actions": []any{map[string]any{"id": "approve", "label": "OK"}},
			}},
		}},
	})
	h.db.Create(&models.StateRun{RunID: runID, NodeID: "upstream", Iteration: 1, Status: "completed"})
	h.db.Create(&models.Gate{
		RunID: runID, NodeID: "gate", Iteration: 1, Title: "Review", RequestedAt: now,
		Actions: []models.GateAction{{ID: "approve", Label: "OK"}},
	})
	w := h.do(http.MethodPost, "/api/runs/"+runID+"/nodes/gate/decision", map[string]any{"action": "approve"})
	if w.Code != http.StatusOK {
		t.Fatalf("resume gate: %d %s", w.Code, w.Body.String())
	}
}

func TestReactReplySuccess(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	runID := "run-react-reply"
	h.db.Create(&models.Run{
		ID: runID, Status: "waiting_human", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{{ID: "react", Type: "agent", Caps: testClarifyCaps, Label: "澄清"}}},
	})
	h.db.Create(&models.ReactConversation{
		RunID: runID, NodeID: "react", Iteration: 1, Done: false,
		Messages: []models.ReactMessage{{Role: "agent", Text: "question?", At: now.Format(time.RFC3339)}},
	})
	h.db.Create(&models.StateRun{RunID: runID, NodeID: "react", Iteration: 1, Status: "waiting_human"})
	w := h.do(http.MethodPost, "/api/runs/"+runID+"/nodes/react/reply", map[string]any{"text": "answer"})
	if w.Code != http.StatusOK {
		t.Fatalf("react reply: %d %s", w.Code, w.Body.String())
	}
}

func TestReactReplyRetryLast(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	runID := "run-react-retry"
	h.db.Create(&models.Run{
		ID: runID, Status: "waiting_human", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{{ID: "react", Type: "agent", Caps: testClarifyCaps, Label: "澄清"}}},
	})
	h.db.Create(&models.ReactConversation{
		RunID: runID, NodeID: "react", Iteration: 1, Done: false,
		Messages: []models.ReactMessage{
			{Role: "human", Text: "做登录", At: now.Format(time.RFC3339)},
			{Role: "agent", Text: "", At: now.Format(time.RFC3339)},
		},
	})
	h.db.Create(&models.StateRun{RunID: runID, NodeID: "react", Iteration: 1, Status: "waiting_human"})
	w := h.do(http.MethodPost, "/api/runs/"+runID+"/nodes/react/reply", map[string]any{"retryLast": true})
	if w.Code != http.StatusOK {
		t.Fatalf("retryLast: %d %s", w.Code, w.Body.String())
	}
}

func TestReactReplyClarifyVisitEndedHTTP(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	runID := "run-react-visit-ended"
	h.db.Create(&models.Run{
		ID: runID, Status: "cancelled", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{{ID: "react", Type: "agent", Caps: testClarifyCaps, Label: "澄清"}}},
	})
	h.db.Create(&models.ReactConversation{
		RunID: runID, NodeID: "react", Iteration: 1, Done: false,
		Messages: []models.ReactMessage{{Role: "agent", Text: "question?", At: now.Format(time.RFC3339)}},
	})
	// Cancel finalizes the StateRun; clarifyVisitLive must refuse new turns.
	h.db.Create(&models.StateRun{RunID: runID, NodeID: "react", Iteration: 1, Status: "cancelled"})
	w := h.do(http.MethodPost, "/api/runs/"+runID+"/nodes/react/reply", map[string]any{
		"text":        "answer",
		"images":      []map[string]any{{"data": "QUJD", "mimeType": "image/png", "name": "a.png"}},
		"annotations": []map[string]any{{"selector": "#x", "note": "改这里"}},
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("react reply after cancel: %d %s", w.Code, w.Body.String())
	}
	body := parseJSON(t, w)
	if body["code"] != "clarify_visit_ended" {
		t.Fatalf("code=%v body=%s", body["code"], w.Body.String())
	}
	if body["error"] != "运行已结束，这条消息没有发送" {
		t.Fatalf("error=%v", body["error"])
	}
	if body["autoRetryOnResume"] != false {
		t.Fatalf("autoRetryOnResume=%v want false", body["autoRetryOnResume"])
	}
	if strings.Contains(w.Body.String(), "续跑后") {
		t.Fatalf("must not embed resume hint: %s", w.Body.String())
	}
	d, _ := body["dropped"].(map[string]any)
	if d == nil || d["text"] != "answer" {
		t.Fatalf("dropped=%v", body["dropped"])
	}
	if a, _ := d["attachments"].([]any); len(a) != 1 || a[0].(map[string]any)["data"] != "QUJD" {
		t.Fatalf("attachments=%v", d["attachments"])
	}
	if a, _ := d["annotations"].([]any); len(a) != 1 || a[0].(map[string]any)["note"] != "改这里" {
		t.Fatalf("annotations=%v", d["annotations"])
	}
	if _, has := body["clientMessageId"]; has {
		t.Fatalf("no id was sent; clientMessageId must be omitted: %s", w.Body.String())
	}

	const cid = "5c9d1e7a-2f4b-4a6c-8e3d-1b7f9a2c6e54"
	w2 := h.do(http.MethodPost, "/api/runs/"+runID+"/nodes/react/reply", map[string]any{"text": "again", "clientMessageId": cid})
	b2 := parseJSON(t, w2)
	if w2.Code != http.StatusConflict || b2["clientMessageId"] != cid {
		t.Fatalf("409 must echo clientMessageId: %d %s", w2.Code, w2.Body.String())
	}
	if d2, _ := b2["dropped"].(map[string]any); d2 == nil || d2["text"] != "again" {
		t.Fatalf("409 keeps dropped: %s", w2.Body.String())
	}
	w3 := h.do(http.MethodPost, "/api/runs/"+runID+"/nodes/react/reply", map[string]any{"text": "x", "clientMessageId": "not-a-uuid"})
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("invalid clientMessageId: %d %s", w3.Code, w3.Body.String())
	}
}

func TestReactReplyRetryLastWithForceRejected(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	runID := "run-react-retry-force"
	h.db.Create(&models.Run{
		ID: runID, Status: "waiting_human", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{{ID: "react", Type: "agent", Caps: testClarifyCaps, Label: "澄清"}}},
	})
	h.db.Create(&models.ReactConversation{
		RunID: runID, NodeID: "react", Iteration: 1, Done: false,
		Messages: []models.ReactMessage{{Role: "human", Text: "x", At: now.Format(time.RFC3339)}},
	})
	w := h.do(http.MethodPost, "/api/runs/"+runID+"/nodes/react/reply", map[string]any{
		"retryLast": true, "force": true,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", w.Code, w.Body.String())
	}
}

func TestImportAgentDefaultMode(t *testing.T) {
	h := newHarness(t)
	seedAgent(t, h, "ZipMode")
	w := h.do(http.MethodGet, "/api/agents/ZipMode/export", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("export: %d", w.Code)
	}
	zipBytes := append([]byte(nil), w.Body.Bytes()...)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("targetName", "ImportedDefaultMode")
	fw, _ := mw.CreateFormFile("file", "agent.zip")
	_, _ = fw.Write(zipBytes)
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/agents/import", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: h.cookie})
	rec := httptest.NewRecorder()
	h.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("import default mode: %d %s", rec.Code, rec.Body.String())
	}
}

func TestResumeRunEngineError(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{
		ID: "r-running", Status: "running", StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{{ID: "in", Type: "input"}}},
	})
	if w := h.do(http.MethodPost, "/api/runs/r-running/resume", map[string]string{"nodeId": "in"}); w.Code != http.StatusBadRequest {
		t.Fatalf("resume running run: %d %s", w.Code, w.Body.String())
	}
}
