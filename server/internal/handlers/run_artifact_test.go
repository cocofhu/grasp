package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/models"
)

func (hn *harness) putArtifact(runID, name, content, ifMatch string, withCookie bool) *httptest.ResponseRecorder {
	b, _ := json.Marshal(map[string]any{"content": content})
	req := httptest.NewRequest(http.MethodPut, "/api/runs/"+runID+"/artifacts/"+name, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}
	if withCookie {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	}
	w := httptest.NewRecorder()
	hn.r.ServeHTTP(w, req)
	return w
}

type runArtifactResp struct {
	ETag     string `json:"etag"`
	NodeID   string `json:"nodeId"`
	Revision int    `json:"revision"`
	Changed  bool   `json:"changed"`
	Code     string `json:"code"`
	Field    string `json:"field"`
	Error    string `json:"error"`
}

func decodeRunArtifact(t *testing.T, w *httptest.ResponseRecorder) runArtifactResp {
	t.Helper()
	var out runArtifactResp
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return out
}

func TestSaveRunArtifactHTTP(t *testing.T) {
	h := newHarness(t)
	runID := "run-http-run-edit"
	now := time.Now()
	h.db.Create(&models.Run{
		ID: runID, WorkflowID: "w", WorkflowName: "w", Status: "failed",
		Graph:     models.Graph{Nodes: []models.Node{{ID: "research", Type: "agent", Caps: testReviewCaps}}},
		StartedAt: now, CreatedAt: now,
	})
	h.db.Create(&models.StateRun{RunID: runID, NodeID: "research", NodeType: "agent", Iteration: 1, Status: "failed"})
	if _, err := h.h.Arts.Save(runID, "research", "notes.md", "markdown", "v1"); err != nil {
		t.Fatal(err)
	}

	detail := h.do(http.MethodGet, "/api/runs/"+runID, nil)
	var d struct {
		ArtifactEdit struct {
			Editable bool   `json:"editable"`
			Reason   string `json:"reason"`
		} `json:"artifactEdit"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &d); err != nil || !d.ArtifactEdit.Editable {
		t.Fatalf("artifactEdit: %s", detail.Body.String())
	}

	if w := h.putArtifact(runID, "notes.md", "v2", "", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous save: %d %s", w.Code, w.Body.String())
	}

	w := h.putArtifact(runID, "notes.md", "v2", "", true)
	res := decodeRunArtifact(t, w)
	if w.Code != 200 || !res.Changed || res.Revision != 2 || res.NodeID != "research" || w.Header().Get("ETag") != res.ETag {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}

	w = h.putArtifact(runID, "notes.md", "v3", `W/"stale"`, true)
	if c := decodeRunArtifact(t, w); w.Code != http.StatusConflict || c.Code != "artifact_conflict" || c.Revision != 2 {
		t.Fatalf("conflict: %d %s", w.Code, w.Body.String())
	}

	w = h.putArtifact(runID, "plan.json", `{"goals":[]}`, "", true)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing: %d %s", w.Code, w.Body.String())
	}
	if _, err := h.h.Arts.Save(runID, "research", "plan.json", "json", `{"goals":[{"id":"g1","title":"A"}]}`); err != nil {
		t.Fatal(err)
	}
	w = h.putArtifact(runID, "plan.json", `{"goals":[]}`, "", true)
	if c := decodeRunArtifact(t, w); w.Code != http.StatusBadRequest || c.Field != "goals" || c.Error == "" {
		t.Fatalf("invalid: %d %s", w.Code, w.Body.String())
	}

	req := httptest.NewRequest(http.MethodPut, "/api/runs/"+runID+"/artifacts/notes.md", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: h.cookie})
	rec := httptest.NewRecorder()
	h.r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing content: %d", rec.Code)
	}

	h.db.Model(&models.Run{}).Where("id = ?", runID).Update("status", "completed")
	w = h.putArtifact(runID, "notes.md", "v4", "", true)
	if c := decodeRunArtifact(t, w); w.Code != http.StatusConflict || c.Code != "run_not_editable" {
		t.Fatalf("ended: %d %s", w.Code, w.Body.String())
	}
	detail = h.do(http.MethodGet, "/api/runs/"+runID, nil)
	_ = json.Unmarshal(detail.Body.Bytes(), &d)
	if d.ArtifactEdit.Editable || d.ArtifactEdit.Reason != "run_ended" {
		t.Fatalf("ended artifactEdit: %+v", d.ArtifactEdit)
	}
}
