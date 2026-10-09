package handlers_test

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
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
		"name": "NoProject",
		"nodes": []map[string]any{
			{"id": "in", "type": "input", "label": "S"},
			{"id": "out", "type": "output", "label": "E"},
		},
		"edges": []map[string]any{{"id": "e", "source": "in", "target": "out"}},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing projectId: %d %s", w.Code, w.Body.String())
	}

	body := map[string]any{
		"name": "DupName", "projectId": models.DefaultProjectID,
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
	w := h.do(http.MethodGet, "/api/runs/run-gate-empty/gates/gate/primary-artifacts", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list primary: %d %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"items":[]`)) {
		t.Fatalf("body: %s", w.Body.String())
	}
}

func TestReactReplyBadBody(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodPost, "/api/runs/r1/react/n1/reply", "bad")
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
	w := h.do(http.MethodPost, "/api/runs/r1/gates/g1/resume", "bad")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("resume gate bad body: %d", w.Code)
	}
}

func TestSaveGateArtifactBadBody(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodPut, "/api/runs/r1/gates/g1/artifacts/x.json", "bad")
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

func TestSaveWorkflowProjectErrors(t *testing.T) {
	h := newHarness(t)
	w := h.do(http.MethodPost, "/api/workflows", map[string]any{
		"name": "BadProject", "projectId": "ghost-project",
		"nodes": []map[string]any{
			{"id": "in", "type": "input", "label": "S"},
			{"id": "out", "type": "output", "label": "E"},
		},
		"edges": []map[string]any{{"id": "e", "source": "in", "target": "out"}},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown project: %d %s", w.Code, w.Body.String())
	}

	seedPublishedWorkflow(t, h, "wf-immut")
	w = h.do(http.MethodPut, "/api/workflows/wf-immut", map[string]any{
		"id": "wf-immut", "name": "Moved", "projectId": "other-project",
		"nodes": []map[string]any{
			{"id": "in", "type": "input", "label": "S"},
			{"id": "out", "type": "output", "label": "E"},
		},
		"edges": []map[string]any{{"id": "e", "source": "in", "target": "out"}},
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("immutable project: %d %s", w.Code, w.Body.String())
	}
}

func TestListGatesBareAndArtifactsPaged(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{ID: "run-gate-bare", Status: "waiting_human", WorkflowID: "wf-1", StartedAt: now})
	h.db.Create(&models.Gate{RunID: "run-gate-bare", NodeID: "gate", Title: "G", RequestedAt: now})
	h.db.Create(&models.Artifact{ProjectID: models.DefaultProjectID, ID: "art-page", RunID: "run-gate-bare", Name: "a.md", Kind: "markdown", CreatedAt: now})

	if w := h.do(http.MethodGet, "/api/gates", nil); w.Code != http.StatusOK {
		t.Fatalf("bare gates: %d %s", w.Code, w.Body.String())
	}
	if w := h.do(http.MethodGet, "/api/artifacts?projectId="+models.DefaultProjectID+"&page=1&pageSize=5", nil); w.Code != http.StatusOK {
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
		"name": "   ", "projectId": models.DefaultProjectID,
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
	w := h.do(http.MethodPost, "/api/runs/"+runID+"/gates/gate/resume", map[string]any{"action": "approve"})
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
	w := h.do(http.MethodPost, "/api/runs/"+runID+"/react/react/reply", map[string]any{"text": "answer"})
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
	w := h.do(http.MethodPost, "/api/runs/"+runID+"/react/react/reply", map[string]any{"retryLast": true})
	if w.Code != http.StatusOK {
		t.Fatalf("retryLast: %d %s", w.Code, w.Body.String())
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
	w := h.do(http.MethodPost, "/api/runs/"+runID+"/react/react/reply", map[string]any{
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
	_ = mw.WriteField("projectId", models.DefaultProjectID)
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
