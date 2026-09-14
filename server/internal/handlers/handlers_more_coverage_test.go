package handlers_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestCopyWorkflowPreviewAndCopyErrors(t *testing.T) {
	hn := newHarness(t)
	seedPublishedWorkflow(t, hn, "src-wf")

	w := hn.do(http.MethodGet, "/api/workflows/missing/copy-preview", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("preview missing: %d", w.Code)
	}
	w = hn.do(http.MethodGet, "/api/workflows/src-wf/copy-preview", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("preview ok: %d %s", w.Code, w.Body.String())
	}

	w = hn.do(http.MethodPost, "/api/workflows/missing/copy", map[string]any{"name": "copy"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("copy missing: %d", w.Code)
	}
	w = hn.do(http.MethodPost, "/api/workflows/src-wf/copy", map[string]any{"name": ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("copy empty name: %d", w.Code)
	}
	w = hn.do(http.MethodPost, "/api/workflows/src-wf/copy", map[string]any{"name": "src-wf-copy"})
	if w.Code != http.StatusCreated {
		t.Fatalf("copy ok: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPost, "/api/workflows/src-wf/copy", map[string]any{"name": "src-wf-copy"})
	if w.Code != http.StatusConflict {
		t.Fatalf("copy duplicate: %d", w.Code)
	}
}

func TestRenameAgentBranches(t *testing.T) {
	hn := newHarness(t)
	seedAgent(t, hn, "OldName")
	w := hn.do(http.MethodPost, "/api/agents/OldName/rename", map[string]any{"name": "NewName"})
	if w.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPost, "/api/agents/Missing/rename", map[string]any{"name": "X"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("rename missing: %d", w.Code)
	}
	seedAgent(t, hn, "Other")
	w = hn.do(http.MethodPost, "/api/agents/NewName/rename", map[string]any{"name": "Other"})
	if w.Code != http.StatusConflict {
		t.Fatalf("rename conflict: %d", w.Code)
	}
	w = hn.do(http.MethodPost, "/api/agents/NewName/rename", map[string]any{"name": ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("rename empty: %d", w.Code)
	}
}

func TestDeleteAgentWithOrgCascade(t *testing.T) {
	hn := newHarness(t)
	enableAdmin(t)
	root := t.TempDir()
	skills := services.NewAgentService(root)
	hn.h.Org = services.NewOrgService(root, skills)
	seedAgent(t, hn, "OrgAgent")
	w := hn.do(http.MethodDelete, "/api/agents/OrgAgent", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete agent: %d %s", w.Code, w.Body.String())
	}
}

func TestArtifactContentReturnsETag(t *testing.T) {
	hn := newHarness(t)
	content := `{"summary":"x"}`
	if _, err := hn.h.Arts.Save("run-et", "node", mcp.ResearchArtifactName, "json", content); err != nil {
		t.Fatal(err)
	}
	var art models.Artifact
	if err := hn.db.Where("run_id = ? AND name = ?", "run-et", mcp.ResearchArtifactName).First(&art).Error; err != nil {
		t.Fatal(err)
	}
	w := hn.do(http.MethodGet, "/api/artifacts/"+art.ID+"/content", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("content: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("ETag") == "" {
		t.Fatal("missing ETag header")
	}
	if !strings.Contains(w.Body.String(), `"revision":1`) {
		t.Fatalf("artifact content should include revision, got %s", w.Body.String())
	}
}

func TestArtifactVersionListAndContent(t *testing.T) {
	hn := newHarness(t)
	if _, err := hn.h.Arts.Save("run-ver", "node", mcp.ResearchArtifactName, "json", `{"v":1}`); err != nil {
		t.Fatal(err)
	}
	if _, err := hn.h.Arts.Save("run-ver", "node", mcp.ResearchArtifactName, "json", `{"v":2}`); err != nil {
		t.Fatal(err)
	}
	var art models.Artifact
	if err := hn.db.Where("run_id = ? AND name = ?", "run-ver", mcp.ResearchArtifactName).First(&art).Error; err != nil {
		t.Fatal(err)
	}

	missing := hn.do(http.MethodGet, "/api/artifacts/missing/versions", nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing versions: %d", missing.Code)
	}
	list := hn.do(http.MethodGet, "/api/artifacts/"+art.ID+"/versions", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("versions: %d %s", list.Code, list.Body.String())
	}
	if !strings.Contains(list.Body.String(), `"revision":1`) {
		t.Fatalf("versions body=%s", list.Body.String())
	}
	if strings.Contains(list.Body.String(), `"content"`) {
		t.Fatalf("version list must omit content: %s", list.Body.String())
	}

	badRev := hn.do(http.MethodGet, "/api/artifacts/"+art.ID+"/versions/x/content", nil)
	if badRev.Code != http.StatusBadRequest {
		t.Fatalf("bad rev: %d", badRev.Code)
	}
	gone := hn.do(http.MethodGet, "/api/artifacts/"+art.ID+"/versions/9/content", nil)
	if gone.Code != http.StatusNotFound {
		t.Fatalf("missing rev: %d", gone.Code)
	}
	body := hn.do(http.MethodGet, "/api/artifacts/"+art.ID+"/versions/1/content", nil)
	if body.Code != http.StatusOK {
		t.Fatalf("version content: %d %s", body.Code, body.Body.String())
	}
	if !strings.Contains(body.Body.String(), `"content":"{\"v\":1}"`) && !strings.Contains(body.Body.String(), `"v":1`) {
		t.Fatalf("version content body=%s", body.Body.String())
	}
}

func TestListGatePrimaryArtifactsBadRun(t *testing.T) {
	hn := newHarness(t)
	w := hn.do(http.MethodGet, "/api/runs/missing/gates/gate/primary-artifacts", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad run: %d %s", w.Code, w.Body.String())
	}
}

func TestListSandboxesAndEventLog(t *testing.T) {
	hn := newHarness(t)
	w := hn.do(http.MethodGet, "/api/sandboxes", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list sandboxes: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodGet, "/api/sandboxes/bad/eventlog", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d %s", w.Code, w.Body.String())
	}
}

func TestPmNilServicePaths(t *testing.T) {
	hn := newHarness(t)
	hn.h.Pm = nil
	w := hn.do(http.MethodGet, "/api/projects/"+models.DefaultProjectID+"/pm/threads", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("nil pm list threads: %d", w.Code)
	}
}
