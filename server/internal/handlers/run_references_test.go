package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

type githubQueries struct {
	mu      sync.Mutex
	queries []string
}

func (g *githubQueries) last() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.queries) == 0 {
		return ""
	}
	return g.queries[len(g.queries)-1]
}

func referenceHarness(t *testing.T) (*harness, *githubQueries) {
	t.Helper()
	hn := newHarness(t)
	solo := services.NewSoloService(hn.db, nil, hn.h.Agents, services.NewProjectService(hn.db))
	gh := &githubQueries{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query().Get("q"); q != "" {
			gh.mu.Lock()
			gh.queries = append(gh.queries, q)
			gh.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":0,"incomplete_results":false,"items":[]}`))
	}))
	t.Cleanup(srv.Close)
	solo.SetGitHubAPIForTest(srv.URL, srv.Client())
	hn.h.Solo = solo
	for _, row := range []models.SoloSession{
		{ID: "mine", UserID: "admin", Title: "登录重构", Status: models.SoloStatusClosed},
		{ID: "bobs", UserID: "bob", Title: "登录 bob", Status: models.SoloStatusClosed},
	} {
		if err := hn.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return hn, gh
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var failure map[string]any
	_ = json.Unmarshal(body, &failure)
	code, _ := failure["code"].(string)
	return code
}

func TestWorkflowReferenceSearchAndLaunch(t *testing.T) {
	hn, gh := referenceHarness(t)
	repos := `[{"name":"web","url":"https://github.com/acme/web.git"}]`
	hn.db.Create(&models.WorkflowDef{ID: "wf-ref", Name: "ref", Version: 1, PublishedVersion: 1, Graph: models.Graph{
		Variables: []models.Variable{{Name: "repos", Type: "paragraph", Value: repos}},
		Nodes:     []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}},
		Edges:     []models.Edge{{ID: "e1", Source: "in", Target: "out"}},
	}})
	hn.db.Create(&models.WorkflowDef{ID: "wf-bare", Name: "bare", Version: 1, PublishedVersion: 1})

	res := hn.do(http.MethodGet, "/api/workflows/wf-ref/references/search?lane=solo", nil)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"target":"mine"`) || strings.Contains(res.Body.String(), `"target":"bobs"`) {
		t.Fatalf("solo lane: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodGet, "/api/workflows/wf-ref/references/search?lane=github&q=bug", nil); res.Code != http.StatusOK {
		t.Fatalf("github lane: %d %s", res.Code, res.Body)
	}
	if q := gh.last(); !strings.Contains(q, "repo:acme/web") {
		t.Fatalf("github scoped to workflow repos: %q", q)
	}
	res = hn.do(http.MethodGet, "/api/workflows/wf-bare/references/search?lane=github&q=bug", nil)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"source":"none"`) {
		t.Fatalf("no repos: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodGet, "/api/workflows/missing/references/search?lane=solo", nil); res.Code != http.StatusNotFound {
		t.Fatalf("unknown workflow: %d", res.Code)
	}

	var before int64
	hn.db.Model(&models.Run{}).Count(&before)
	res = hn.do(http.MethodPost, "/api/workflows/wf-ref/runs", map[string]any{
		"firstMessage": map[string]any{"text": "看看", "references": []map[string]string{{"kind": "solo", "target": "bobs"}}},
	})
	if res.Code != http.StatusNotFound || errorCode(t, res.Body.Bytes()) != "reference_not_found" {
		t.Fatalf("foreign reference: %d %s", res.Code, res.Body)
	}
	var after int64
	hn.db.Model(&models.Run{}).Count(&after)
	if after != before {
		t.Fatal("a rejected reference created a run")
	}
	res = hn.do(http.MethodPost, "/api/workflows/wf-ref/runs", map[string]any{
		"firstMessage": map[string]any{"references": []map[string]string{{"kind": "solo", "target": "mine"}}},
	})
	if res.Code != http.StatusOK {
		t.Fatalf("start with reference: %d %s", res.Code, res.Body)
	}
	var started struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(res.Body.Bytes(), &started)
	var run models.Run
	if err := hn.db.First(&run, "id = ?", started.ID).Error; err != nil {
		t.Fatal(err)
	}
	if run.Title != "@登录重构" || run.FirstMessage == nil || run.FirstMessage.ReferenceUser != "admin" ||
		len(run.FirstMessage.References) != 1 || run.FirstMessage.References[0].Target != "mine" {
		t.Fatalf("stored run: %q %+v", run.Title, run.FirstMessage)
	}
}

func TestRunNodeReferenceSearchAndReply(t *testing.T) {
	hn, gh := referenceHarness(t)
	runID := "run-refs"
	seedDialogueRun(hn, runID)
	hn.db.Create(&models.RunVariable{RunID: runID, Name: "repos", Type: "paragraph", Value: `[{"name":"api","url":"https://github.com/acme/api.git"}]`})

	if res := hn.do(http.MethodGet, "/api/runs/"+runID+"/nodes/react/references/search?lane=github&q=bug", nil); res.Code != http.StatusOK {
		t.Fatalf("github lane: %d %s", res.Code, res.Body)
	}
	if q := gh.last(); !strings.Contains(q, "repo:acme/api") {
		t.Fatalf("github scoped to run repos: %q", q)
	}
	res := hn.do(http.MethodGet, "/api/runs/"+runID+"/nodes/react/references/search?lane=solo&q="+url.QueryEscape("登录"), nil)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"target":"mine"`) || strings.Contains(res.Body.String(), `"target":"bobs"`) {
		t.Fatalf("solo lane: %d %s", res.Code, res.Body)
	}
	for _, path := range []string{"/api/runs/missing/nodes/react/references/search?lane=solo", "/api/runs/" + runID + "/nodes/nope/references/search?lane=solo"} {
		if res := hn.do(http.MethodGet, path, nil); res.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", path, res.Code)
		}
	}

	refs := []map[string]string{{"kind": "solo", "target": "mine"}}
	res = hn.do(http.MethodPost, "/api/runs/"+runID+"/nodes/react/reply", map[string]any{"retryLast": true, "references": refs})
	if res.Code != http.StatusBadRequest || errorCode(t, res.Body.Bytes()) != "reference_not_supported" {
		t.Fatalf("retryLast with references: %d %s", res.Code, res.Body)
	}
	res = hn.do(http.MethodPost, "/api/runs/"+runID+"/nodes/react/reply", map[string]any{"text": "x", "references": []map[string]string{{"kind": "solo", "target": "bobs"}}})
	if res.Code != http.StatusNotFound || errorCode(t, res.Body.Bytes()) != "reference_not_found" {
		t.Fatalf("foreign reference on reply: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodPost, "/api/runs/"+runID+"/nodes/react/reply", map[string]any{"text": "x", "references": []map[string]string{{"kind": "jira", "target": "x"}}}); res.Code != http.StatusBadRequest {
		t.Fatalf("bad kind: %d %s", res.Code, res.Body)
	}
}
