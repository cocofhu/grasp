package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
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

func TestWorkflowLaunchReferenceRetryUsesOriginalIntent(t *testing.T) {
	for _, channel := range []string{"web", "solo"} {
		t.Run(channel, func(t *testing.T) {
			hn, _ := referenceHarness(t)
			seedPublishedWorkflow(t, hn, "wf-ref-retry")
			if err := hn.db.Create(&models.SoloSession{ID: "launch-owner", UserID: "admin", Status: models.SoloStatusClosed}).Error; err != nil {
				t.Fatal(err)
			}
			var githubCalls atomic.Int32
			github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				githubCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"number":12,"title":"Original issue","state":"open","html_url":"https://github.com/acme/web/issues/12"}`))
			}))
			t.Cleanup(github.Close)
			hn.h.Solo.SetGitHubAPIForTest(github.URL, github.Client())
			path := "/api/workflows/wf-ref-retry/runs"
			if channel == "solo" {
				path = "/api/solo/sessions/launch-owner/workflows/wf-ref-retry/runs"
			}
			body := map[string]any{
				"confirmed": true, "expectedWorkflowVersion": 1,
				"firstMessage": map[string]any{"text": "看看引用", "references": []map[string]string{
					{"kind": "solo", "target": "mine"}, {"kind": models.ReferenceKindGitHubIssue, "target": "acme/web#12"},
				}},
			}
			post := func(key string) *httptest.ResponseRecorder {
				t.Helper()
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Idempotency-Key", key)
				req.AddCookie(&http.Cookie{Name: "cf_session", Value: hn.cookie})
				hn.r.ServeHTTP(w, req)
				return w
			}
			first := post("reference-click")
			if first.Code != http.StatusOK {
				t.Fatalf("initial launch: %d %s", first.Code, first.Body)
			}
			var admitted struct{ ID string }
			if err := json.Unmarshal(first.Body.Bytes(), &admitted); err != nil || admitted.ID == "" {
				t.Fatalf("missing launch receipt: %s %v", first.Body, err)
			}
			if githubCalls.Load() != 1 {
				t.Fatalf("GitHub validation calls: %d", githubCalls.Load())
			}
			if err := hn.db.Model(&models.SoloSession{}).Where("id = ?", "mine").Update("title", "renamed after launch").Error; err != nil {
				t.Fatal(err)
			}
			for attempt := range 2 {
				if attempt == 1 {
					if err := hn.db.Delete(&models.SoloSession{}, "id = ?", "mine").Error; err != nil {
						t.Fatal(err)
					}
				}
				retry := post("reference-click")
				var replay struct{ ID string }
				_ = json.Unmarshal(retry.Body.Bytes(), &replay)
				if retry.Code != http.StatusOK || replay.ID != admitted.ID {
					t.Fatalf("retry %d: %d %s", attempt, retry.Code, retry.Body)
				}
			}
			if githubCalls.Load() != 1 {
				t.Fatal("receipt replay revalidated external metadata")
			}
			stored, ok := hn.h.Runs.Get(admitted.ID)
			if !ok || stored.FirstMessage == nil || len(stored.FirstMessage.References) != 2 ||
				stored.FirstMessage.References[0].Title != "登录重构" || stored.FirstMessage.References[1].State != "open" {
				t.Fatalf("first reference snapshot was replaced: %+v", stored.FirstMessage)
			}
			body["firstMessage"].(map[string]any)["text"] = "改变正文"
			conflict := post("reference-click")
			if conflict.Code != http.StatusConflict || errorCode(t, conflict.Body.Bytes()) != "idempotency_conflict" {
				t.Fatalf("changed intent: %d %s", conflict.Code, conflict.Body)
			}
			// Only a stored receipt bypasses re-resolution. A fresh launch must
			// still reject a missing or unauthorized reference.
			fresh := post("new-reference-click")
			if fresh.Code != http.StatusNotFound || errorCode(t, fresh.Body.Bytes()) != "reference_not_found" {
				t.Fatalf("fresh invalid reference: %d %s", fresh.Code, fresh.Body)
			}
			var launches, runs int64
			hn.db.Model(&models.WorkflowLaunch{}).Count(&launches)
			hn.db.Model(&models.Run{}).Count(&runs)
			if launches != 1 || runs != 1 {
				t.Fatalf("duplicate launch: receipts=%d runs=%d", launches, runs)
			}
		})
	}
}
