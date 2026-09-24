package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/embed"
	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/handlers"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/router"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/sandbox/sandboxtest"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// fakeProvider is a deterministic, Docker-free ExecProvider for handler tests.
type fakeProvider struct{}

func (fakeProvider) Name() string { return "fake" }
func (fakeProvider) RunAgent(ctx context.Context, req runtime.NodeReq) (runtime.NodeResult, error) {
	return runtime.NodeResult{OutputMd: "done", Outputs: map[string]any{}}, nil
}
func (fakeProvider) ReactOpen(ctx context.Context, req runtime.NodeReq) runtime.ReactTurn {
	return runtime.ReactTurn{Done: true, Result: runtime.NodeResult{Outputs: map[string]any{}}}
}
func (fakeProvider) ReactReply(ctx context.Context, req runtime.NodeReq, history []models.ReactMessage, human string, images []models.PromptImage, force bool) runtime.ReactTurn {
	return runtime.ReactTurn{Done: true, Result: runtime.NodeResult{Outputs: map[string]any{}}}
}

type harness struct {
	r      *gin.Engine
	h      *handlers.Handlers
	db     *gorm.DB
	host   *mcp.Host
	auth   *auth.Service
	cookie string
	fg     *sandboxtest.FakeGateway
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := database.OpenSQLiteTest(t.TempDir() + "/h.db")
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	profilesRoot := t.TempDir()
	skills := services.NewAgentService(profilesRoot)
	globalRules := t.TempDir() + "/platform-rules"
	platformRules, err := services.NewPlatformRuleService(globalRules, profilesRoot)
	if err != nil {
		t.Fatalf("platform rules: %v", err)
	}
	fg := sandboxtest.New(t)
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	sbx := services.NewSandboxService(db, mgr, skills, host, services.SandboxOptions{Max: 2, TTL: time.Minute})
	eng := engine.New(db, fakeProvider{}, host, arts, 5)
	auditSvc := services.NewProjectAuditService(db)
	eng.SetAuditRecorder(func(rec services.AuditRecord) {
		auditSvc.Record(rec)
	})
	gateShareSvc := gateshare.NewService(db, auditSvc)
	gateShareTickets := gateshare.NewTicketStore(db)
	gateShareSessions := gateshare.NewPreviewSessionHub()
	embedStore := embed.NewStore(db)
	gateShareSvc.SetEmbedLookup(func(token string) (string, bool) {
		c, ok := embedStore.LookupSession(token)
		if !ok || c.Kind != models.EmbedKindShare {
			return "", false
		}
		return c.ShareTokenHash, true
	})
	gateShareSvc.SetInvalidationHook(func(tokenHashes []string) {
		for _, th := range tokenHashes {
			gateShareTickets.InvalidateByTokenHash(th)
		}
		embedStore.InvalidateShare(tokenHashes...)
		gateShareSessions.KickMany(tokenHashes)
	})
	eng.SetShareRevoker(gateShareSvc)
	t.Cleanup(func() {
		eng.Close()
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	cfg := &config.Config{
		Auth: config.AuthConfig{
			Users: []config.AuthUser{
				{Username: "admin", PasswordHash: "$2a$10$EY.SdHq0p6drMz6U9JVrz.Kq0jNkg7TWmsVUFLtB1dL1yIelDkITi"},
			},
			MaxFailures:  100,
			LockDuration: "1m",
			SessionTTL:   "168h",
		},
	}
	config.StoreConfig(cfg)
	authSvc := auth.NewService(db, config.GetConfig)
	projectSvc := services.NewProjectService(db)
	wfSvc := services.NewWorkflowService(db)
	sharedAgent := services.NewSharedAgentService(t.TempDir())
	h := &handlers.Handlers{
		WF:               wfSvc,
		Projects:         projectSvc,
		Runs:             services.NewRunService(db),
		Arts:             arts,
		APIKeys:          services.NewAPIKeyService(db),
		Agents:           skills,
		SharedAgent:      sharedAgent,
		Dash:             services.NewDashboardService(db, projectSvc),
		Sbx:              sbx,
		Eng:              eng,
		MCP:              host,
		Auth:             authSvc,
		PlatformRules:    platformRules,
		Issues:           services.NewIssueService(db),
		Audit:            auditSvc,
		Onboarding:       services.NewOnboardingService(projectSvc, skills, sharedAgent, wfSvc, services.NewOrgService(profilesRoot, skills)),
		GateShare:         gateShareSvc,
		GateShareNonces:   gateshare.NewNonceStore(db),
		GateShareTickets:  gateShareTickets,
		GateShareSessions: gateShareSessions,
		GateShareLimiter:  gateshare.NewIPLimiter(),
		Embed:             embedStore,
		PublicAdvertise:   "http://example.test",
	}
	hn := &harness{r: router.New(h), h: h, db: db, host: host, auth: authSvc, fg: fg}
	hn.cookie = hn.login(t)
	return hn
}

func (hn *harness) login(t *testing.T) string {
	t.Helper()
	w := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "demo1234"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	hn.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}

func (hn *harness) do(method, path string, body any) *httptest.ResponseRecorder {
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	if hn.cookie != "" && !isAuthWhitelist(path) {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	}
	w := httptest.NewRecorder()
	hn.r.ServeHTTP(w, req)
	return w
}

func isAuthWhitelist(path string) bool {
	switch path {
	case "/api/health", "/api/live", "/api/auth/login", "/api/auth/logout":
		return true
	default:
		return false
	}
}

func TestHealthAndDashboard(t *testing.T) {
	h := newHarness(t)
	if w := h.do("GET", "/api/health", nil); w.Code != 200 {
		t.Fatalf("health: %d", w.Code)
	}
	if w := h.do("GET", "/api/stats/dashboard", nil); w.Code != 200 {
		t.Fatalf("dashboard: %d", w.Code)
	}
	w := h.do("GET", "/api/stats/platform-status?utcOffsetMinutes=480", nil)
	if w.Code != 200 {
		t.Fatalf("platform-status: %d %s", w.Code, w.Body.String())
	}
	var st map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"runningCount", "queuedCount", "asOf", "timezone", "cumulativeTokens", "todayTokens"} {
		if _, ok := st[key]; !ok {
			t.Fatalf("platform-status missing %s: %v", key, st)
		}
	}
}

func TestAuthEndpoints(t *testing.T) {
	h := newHarness(t)
	if w := h.doWithCookie("GET", "/api/workflows", nil, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth workflows: %d", w.Code)
	}
	cookie := h.cookie
	if w := h.doWithCookie("GET", "/api/workflows", nil, cookie); w.Code != 200 {
		t.Fatalf("auth workflows: %d %s", w.Code, w.Body.String())
	}
	if w := h.doWithCookie("POST", "/api/auth/logout", nil, cookie); w.Code != 200 {
		t.Fatalf("logout: %d", w.Code)
	}
	if w := h.doWithCookie("GET", "/api/auth/me", nil, cookie); w.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: %d", w.Code)
	}
}

func (hn *harness) httpGet(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if hn.cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: hn.cookie})
	}
	return http.DefaultClient.Do(req)
}

func (hn *harness) wsHeader() http.Header {
	h := http.Header{}
	if hn.cookie != "" {
		h.Add("Cookie", auth.CookieName+"="+hn.cookie)
	}
	return h
}

func (hn *harness) doWithCookie(method, path string, body any, cookie string) *httptest.ResponseRecorder {
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	}
	w := httptest.NewRecorder()
	hn.r.ServeHTTP(w, req)
	return w
}

func TestNodeRegistryEndpoint(t *testing.T) {
	h := newHarness(t)
	w := h.do("GET", "/api/node-registry", nil)
	if w.Code != 200 {
		t.Fatalf("node-registry: %d", w.Code)
	}
	var body struct {
		OutputKeyToArtifact  map[string]string `json:"outputKeyToArtifact"`
		ArtifactToOutputJSON map[string]string `json:"artifactToOutputJSON"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.OutputKeyToArtifact["research"] != "research.json" {
		t.Fatalf("research mapping: %v", body.OutputKeyToArtifact)
	}
	if body.ArtifactToOutputJSON["test_result.json"] != "test_result_json" {
		t.Fatalf("test json mapping: %v", body.ArtifactToOutputJSON)
	}
}

func TestWorkflowEndpoints(t *testing.T) {
	h := newHarness(t)

	// Empty list.
	if w := h.do("GET", "/api/workflows", nil); w.Code != 200 {
		t.Fatalf("list: %d", w.Code)
	}
	// Bad body.
	if w := h.do("POST", "/api/workflows", "not-json"); w.Code != 400 {
		t.Fatalf("bad body: %d", w.Code)
	}
	// Create.
	body := map[string]any{
		"name": "WF", "projectId": models.DefaultProjectID,
		"nodes": []map[string]any{
			{"id": "in", "type": "input"},
			{"id": "out", "type": "output"},
		},
		"edges": []map[string]any{{"id": "e", "source": "in", "target": "out"}},
	}
	w := h.do("POST", "/api/workflows", body)
	if w.Code != 200 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var created map[string]any
	json.Unmarshal(w.Body.Bytes(), &created)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("no id")
	}

	if w := h.do("GET", "/api/workflows/"+id, nil); w.Code != 200 {
		t.Fatalf("get: %d", w.Code)
	}
	if w := h.do("GET", "/api/workflows/ghost", nil); w.Code != 404 {
		t.Fatalf("get missing: %d", w.Code)
	}
	// Update via PUT.
	if w := h.do("PUT", "/api/workflows/"+id, body); w.Code != 200 {
		t.Fatalf("put: %d", w.Code)
	}
	// Publish.
	if w := h.do("POST", "/api/workflows/"+id+"/publish", nil); w.Code != 200 {
		t.Fatalf("publish: %d %s", w.Code, w.Body)
	}
	if w := h.do("GET", "/api/workflows/"+id+"/versions", nil); w.Code != 200 {
		t.Fatalf("versions: %d", w.Code)
	}
	if w := h.do("POST", "/api/workflows/"+id+"/versions/2/restore", nil); w.Code != 200 {
		t.Fatalf("restore: %d", w.Code)
	}
	if w := h.do("POST", "/api/workflows/"+id+"/versions/bad/restore", nil); w.Code != 400 {
		t.Fatalf("restore bad ver: %d", w.Code)
	}
	if w := h.do("POST", "/api/workflows/ghost/publish", nil); w.Code != 400 {
		t.Fatalf("publish missing: %d", w.Code)
	}

	// Start a run against the published workflow.
	if w := h.do("POST", "/api/workflows/"+id+"/runs", map[string]any{"trigger": "manual"}); w.Code != 200 {
		t.Fatalf("start run: %d %s", w.Code, w.Body)
	}
	// Start against missing workflow.
	if w := h.do("POST", "/api/workflows/ghost/runs", nil); w.Code != 400 {
		t.Fatalf("start missing: %d", w.Code)
	}

	// Delete.
	if w := h.do("DELETE", "/api/workflows/"+id, nil); w.Code != 200 {
		t.Fatalf("delete: %d", w.Code)
	}
}

func TestRunEndpoints(t *testing.T) {
	h := newHarness(t)
	// Seed a run + dependents directly.
	now := time.Now()
	h.db.Create(&models.Run{ID: "r1", Status: "completed", StartedAt: now, Graph: models.Graph{
		Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}},
	}})
	h.db.Create(&models.StateRun{RunID: "r1", NodeID: "in", Iteration: 1, Status: "completed", Events: []models.AcpEvent{{Kind: "message", Text: "hi"}}})
	h.db.Create(&models.RunVariable{RunID: "r1", Name: "x", Value: "y"})
	h.db.Create(&models.Artifact{ID: "a1", RunID: "r1", Name: "doc", Content: "c"})

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/runs", 200},
		{"/api/runs/r1", 200},
		{"/api/runs/ghost", 404},
		{"/api/runs/r1/variables", 200},
		{"/api/runs/r1/artifacts", 200},
		{"/api/runs/r1/nodes/in/events", 200},
		{"/api/runs/r1/nodes/ghost/events", 200},
		{"/api/runs/r1/nodes/in/sandbox-log", 200},
	} {
		if w := h.do("GET", tc.path, nil); w.Code != tc.want {
			t.Fatalf("%s = %d, want %d", tc.path, w.Code, tc.want)
		}
	}

	// completed stays rejected; cancelled/failed accept idempotent heal (200).
	if w := h.do("POST", "/api/runs/r1/cancel", nil); w.Code != 400 {
		t.Fatalf("cancel completed run: %d, want 400", w.Code)
	}
	h.db.Create(&models.Run{ID: "r2", Status: "running", StartedAt: now, Graph: models.Graph{
		Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}},
	}})
	if w := h.do("POST", "/api/runs/r2/cancel", nil); w.Code != 200 {
		t.Fatalf("cancel running run: %d", w.Code)
	}
	// Re-cancel after cancelled is a heal (clears sticky StateRuns / zombie slot).
	if w := h.do("POST", "/api/runs/r2/cancel", nil); w.Code != 200 {
		t.Fatalf("cancel heal on cancelled run: %d, want 200", w.Code)
	}
	h.db.Create(&models.Run{ID: "r-failed", Status: "failed", StartedAt: now, Graph: models.Graph{
		Nodes: []models.Node{{ID: "in", Type: "input"}},
	}})
	h.db.Create(&models.StateRun{RunID: "r-failed", NodeID: "in", Iteration: 1, Status: "running"})
	if w := h.do("POST", "/api/runs/r-failed/cancel", nil); w.Code != 200 {
		t.Fatalf("cancel heal on failed run: %d, want 200", w.Code)
	}
	var sr models.StateRun
	h.db.Where("run_id = ? AND node_id = ?", "r-failed", "in").First(&sr)
	if sr.Status != "failed" {
		t.Fatalf("heal StateRun status = %q, want failed", sr.Status)
	}
	if w := h.do("POST", "/api/runs/ghost/cancel", nil); w.Code != 404 {
		t.Fatalf("cancel unknown run: %d", w.Code)
	}
	// Resume gate: bad body, then unknown run.
	if w := h.do("POST", "/api/runs/r1/gates/g/resume", "bad"); w.Code != 400 {
		t.Fatalf("resume bad body: %d", w.Code)
	}
	if w := h.do("POST", "/api/runs/ghost/gates/g/resume", map[string]any{"action": "approve"}); w.Code != 400 {
		t.Fatalf("resume unknown: %d", w.Code)
	}
	if w := h.do("POST", "/api/runs/r1/react/n/reply", "bad"); w.Code != 400 {
		t.Fatalf("react bad body: %d", w.Code)
	}
	if w := h.do("POST", "/api/runs/ghost/react/n/reply", map[string]any{"text": "hi"}); w.Code != 400 {
		t.Fatalf("react unknown: %d", w.Code)
	}
}

func TestRunPriorityEndpoints(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	g := models.Graph{
		Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}},
	}
	h.db.Create(&models.WorkflowDef{ID: "wf-pri", Name: "pri", Status: "published", Version: 1, Graph: g})
	h.db.Create(&models.Run{
		ID: "rp-q", Status: "queued", Priority: models.PriorityNormal, StartedAt: now, Graph: g,
	})
	h.db.Create(&models.Run{
		ID: "rp-done", Status: "completed", Priority: models.PriorityHigh, StartedAt: now, Graph: g,
	})

	w := h.do("PATCH", "/api/runs/rp-q/priority", map[string]any{"priority": "high"})
	if w.Code != 200 {
		t.Fatalf("patch queued priority: %d %s", w.Code, w.Body)
	}
	var okBody map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &okBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if okBody["priority"] != "high" {
		t.Fatalf("priority = %v, want high", okBody["priority"])
	}

	w = h.do("PATCH", "/api/runs/rp-done/priority", map[string]any{"priority": "low"})
	if w.Code != 400 {
		t.Fatalf("terminal priority: %d, want 400", w.Code)
	}

	w = h.do("PATCH", "/api/runs/rp-q/priority", map[string]any{"priority": "urgent"})
	if w.Code != 400 {
		t.Fatalf("invalid priority: %d, want 400", w.Code)
	}

	w = h.do("PATCH", "/api/runs/ghost/priority", map[string]any{"priority": "high"})
	if w.Code != 404 {
		t.Fatalf("missing run: %d, want 404", w.Code)
	}

	// Start run with high priority via internal API.
	w = h.do("POST", "/api/workflows/wf-pri/runs", map[string]any{"trigger": "manual", "priority": "high"})
	if w.Code != 200 {
		t.Fatalf("start with priority: %d %s", w.Code, w.Body)
	}
	var startBody map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &startBody)
	if startBody["priority"] != "high" {
		t.Fatalf("start priority = %v, want high", startBody["priority"])
	}

	// List/detail DTO includes priority string.
	w = h.do("GET", "/api/runs/rp-q", nil)
	if w.Code != 200 {
		t.Fatalf("get run: %d", w.Code)
	}
	var detail map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &detail)
	if detail["priority"] != "high" {
		t.Fatalf("detail priority = %v, want high", detail["priority"])
	}
}

func TestGateAndArtifactEndpoints(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{
		ID: "r1", WorkflowID: "wf1", WorkflowName: "WF", Status: "waiting_human",
		StartedAt: now, Graph: models.Graph{
			Nodes: []models.Node{{ID: "react", Type: "react", Label: "需求澄清"}},
		},
	})
	h.db.Create(&models.Gate{RunID: "r1", NodeID: "g", Resolved: false, RequestedAt: now})
	h.db.Create(&models.StateRun{RunID: "r1", NodeID: "g", Iteration: 0, Status: "waiting_human"})
	h.db.Create(&models.ReactConversation{
		RunID: "r1", NodeID: "react", Iteration: 1, Done: false,
		Messages: []models.ReactMessage{{Role: "agent", Text: "hi", At: now.Format(time.RFC3339)}},
	})
	h.db.Create(&models.StateRun{RunID: "r1", NodeID: "react", Iteration: 1, Status: "waiting_human"})
	h.db.Create(&models.Artifact{ID: "art1", RunID: "r1", Name: "doc", Content: "hello", Kind: "markdown"})

	w := h.do("GET", "/api/gates", nil)
	if w.Code != 200 {
		t.Fatalf("gates: %d", w.Code)
	}
	var items []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode gates: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected gate+clarify union, got %d items", len(items))
	}
	types := map[string]bool{}
	for _, it := range items {
		types[it["type"].(string)] = true
	}
	if !types["gate"] || !types["clarify"] {
		t.Fatalf("missing union types: %+v", items)
	}
	if w := h.do("GET", "/api/artifacts", nil); w.Code != 200 {
		t.Fatalf("artifacts: %d", w.Code)
	}
	if w := h.do("GET", "/api/artifacts/art1/content", nil); w.Code != 200 {
		t.Fatalf("art content: %d", w.Code)
	}
	if w := h.do("GET", "/api/artifacts/ghost/content", nil); w.Code != 404 {
		t.Fatalf("art content missing: %d", w.Code)
	}
	w = h.do("GET", "/api/artifacts/art1/download", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "hello") {
		t.Fatalf("download: %d %s", w.Code, w.Body)
	}
	if w := h.do("GET", "/api/artifacts/ghost/download", nil); w.Code != 404 {
		t.Fatalf("download missing: %d", w.Code)
	}
	// Non-terminal run refuses delete (409) and keeps the record.
	w = h.do(http.MethodDelete, "/api/artifacts/art1", nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("delete non-terminal: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "has not ended") {
		t.Fatalf("409 body should mention run not ended: %s", w.Body)
	}
	if w := h.do("GET", "/api/artifacts/art1/content", nil); w.Code != 200 {
		t.Fatalf("art1 should still exist after 409: %d", w.Code)
	}
}

func TestDeleteRun(t *testing.T) {
	h := newHarness(t)
	now := time.Now()

	if w := h.do(http.MethodDelete, "/api/runs/ghost", nil); w.Code != http.StatusNotFound {
		t.Fatalf("missing: %d %s", w.Code, w.Body)
	}

	for _, status := range []string{"queued", "running", "waiting_human"} {
		id := "del-blocked-" + status
		h.db.Create(&models.Run{ID: id, WorkflowID: "wf-del", Status: status, StartedAt: now})
		w := h.do(http.MethodDelete, "/api/runs/"+id, nil)
		if w.Code != http.StatusConflict {
			t.Fatalf("status %s: %d %s", status, w.Code, w.Body)
		}
		if w := h.do("GET", "/api/runs/"+id, nil); w.Code != 200 {
			t.Fatalf("status %s still gettable: %d", status, w.Code)
		}
	}

	for _, status := range []string{"completed", "failed", "cancelled"} {
		id := "del-ok-" + status
		h.db.Create(&models.Run{ID: id, WorkflowID: "wf-del", Status: status, StartedAt: now})
		h.db.Create(&models.Artifact{ID: "art-" + id, RunID: id, Name: "doc", Content: "x", CreatedAt: now})
		h.db.Create(&models.PreviewIssue{ID: "pi-" + id, RunID: id, NodeID: "p", Body: "x"})

		w := h.do(http.MethodDelete, "/api/runs/"+id, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status %s delete: %d %s", status, w.Code, w.Body)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("status %s body: %v", status, err)
		}
		if body["status"] != "deleted" {
			t.Fatalf("status %s response: %#v", status, body)
		}
		if w := h.do("GET", "/api/runs/"+id, nil); w.Code != http.StatusNotFound {
			t.Fatalf("status %s detail after delete: %d", status, w.Code)
		}
		// Second delete is a safe not-found.
		if w := h.do(http.MethodDelete, "/api/runs/"+id, nil); w.Code != http.StatusNotFound {
			t.Fatalf("status %s second delete: %d", status, w.Code)
		}
		var n int64
		h.db.Model(&models.Artifact{}).Where("run_id = ?", id).Count(&n)
		if n != 0 {
			t.Fatalf("status %s artifacts orphaned: %d", status, n)
		}
		h.db.Model(&models.PreviewIssue{}).Where("run_id = ?", id).Count(&n)
		if n != 0 {
			t.Fatalf("status %s preview issues orphaned: %d", status, n)
		}
	}

	// Deleted run must not appear in the runs list.
	list := h.do("GET", "/api/runs?wf=wf-del", nil)
	if list.Code != 200 {
		t.Fatalf("list: %d", list.Code)
	}
	if strings.Contains(list.Body.String(), "del-ok-completed") ||
		strings.Contains(list.Body.String(), "del-ok-failed") ||
		strings.Contains(list.Body.String(), "del-ok-cancelled") {
		t.Fatalf("deleted runs still listed: %s", list.Body.String())
	}
}

func TestDeleteArtifact(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	for _, status := range []string{"completed", "failed", "cancelled"} {
		runID := "run-" + status
		artID := "art-" + status
		h.db.Create(&models.Run{ID: runID, Status: status, StartedAt: now})
		h.db.Create(&models.Artifact{
			ID: artID, RunID: runID, Name: "doc.md", Content: "x-" + status, Kind: "markdown", CreatedAt: now,
		})
		w := h.do(http.MethodDelete, "/api/artifacts/"+artID, nil)
		if w.Code != http.StatusNoContent {
			t.Fatalf("delete %s: %d %s", status, w.Code, w.Body)
		}
		if w.Body.Len() != 0 {
			t.Fatalf("204 must have empty body for %s, got %q", status, w.Body.String())
		}
		if w := h.do("GET", "/api/artifacts/"+artID+"/content", nil); w.Code != 404 {
			t.Fatalf("content after delete %s: %d", status, w.Code)
		}
		if w := h.do("GET", "/api/artifacts/"+artID+"/download", nil); w.Code != 404 {
			t.Fatalf("download after delete %s: %d", status, w.Code)
		}
	}

	if w := h.do(http.MethodDelete, "/api/artifacts/ghost", nil); w.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d %s", w.Code, w.Body)
	}

	// Unrelated artifact still listed / readable.
	h.db.Create(&models.Run{ID: "run-keep", Status: "completed", StartedAt: now})
	h.db.Create(&models.Artifact{
		ID: "art-keep", RunID: "run-keep", Name: "keep.md", Content: "stay", Kind: "markdown", CreatedAt: now,
	})
	w := h.do("GET", "/api/artifacts", nil)
	if w.Code != 200 {
		t.Fatalf("list: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "art-keep") {
		t.Fatalf("list should still include art-keep: %s", w.Body)
	}
	if strings.Contains(w.Body.String(), "art-completed") {
		t.Fatalf("list must not include deleted art-completed: %s", w.Body)
	}
	if w := h.do("GET", "/api/artifacts/art-keep/content", nil); w.Code != 200 {
		t.Fatalf("keep content: %d", w.Code)
	}
}

func TestListArtifactsRunTitle(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{ID: "run-titled", Title: "需求澄清 Run", StartedAt: now})
	h.db.Create(&models.Run{ID: "run-empty", StartedAt: now})
	h.db.Create(&models.Artifact{ID: "art-titled", RunID: "run-titled", Name: "a.json", Kind: "json", CreatedAt: now})
	h.db.Create(&models.Artifact{ID: "art-empty", RunID: "run-empty", Name: "b.json", Kind: "json", CreatedAt: now})
	h.db.Create(&models.Artifact{ID: "art-orphan", RunID: "run-deleted", Name: "c.json", Kind: "json", CreatedAt: now})

	w := h.do("GET", "/api/artifacts", nil)
	if w.Code != 200 {
		t.Fatalf("artifacts: %d", w.Code)
	}
	var items []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := map[string]map[string]any{}
	for _, it := range items {
		id, _ := it["id"].(string)
		byID[id] = it
	}
	if got, _ := byID["art-titled"]["runTitle"].(string); got != "需求澄清 Run" {
		t.Fatalf("art-titled runTitle = %q, want 需求澄清 Run", got)
	}
	if _, ok := byID["art-empty"]["runTitle"]; ok {
		t.Fatalf("art-empty should omit empty runTitle, got %v", byID["art-empty"]["runTitle"])
	}
	if _, ok := byID["art-orphan"]["runTitle"]; ok {
		t.Fatalf("art-orphan should omit runTitle when run is missing, got %v", byID["art-orphan"]["runTitle"])
	}
}

func TestAgentEndpoints(t *testing.T) {
	h := newHarness(t)
	if w := h.do("GET", "/api/agents", nil); w.Code != 200 {
		t.Fatalf("list: %d", w.Code)
	}
	// Create bad body.
	if w := h.do("POST", "/api/agents", "bad"); w.Code != 400 {
		t.Fatalf("create bad: %d", w.Code)
	}
	// Create no name.
	if w := h.do("POST", "/api/agents", map[string]any{"name": ""}); w.Code != 400 {
		t.Fatalf("create no name: %d", w.Code)
	}
	// Create ok.
	if w := h.do("POST", "/api/agents", map[string]any{"name": "a1"}); w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	// Conflict.
	if w := h.do("POST", "/api/agents", map[string]any{"name": "a1"}); w.Code != 409 {
		t.Fatalf("conflict: %d", w.Code)
	}
	// plan g2.1 / g3.2 — optional templateId copies TestAgent / PreflightAgent workspace
	if w := h.do("POST", "/api/agents", map[string]any{
		"name": "qa-test", "templateId": "test", "acpBackend": "cursor",
	}); w.Code != 201 {
		t.Fatalf("create test template: %d %s", w.Code, w.Body)
	}
	if w := h.do("GET", "/api/agents/qa-test", nil); w.Code != 200 {
		t.Fatalf("get qa-test: %d", w.Code)
	} else {
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		files, _ := body["files"].([]any)
		if len(files) == 0 {
			t.Fatal("qa-test should have template files")
		}
	}
	if w := h.do("POST", "/api/agents", map[string]any{
		"name": "qa-pf", "templateId": "preflight",
	}); w.Code != 201 {
		t.Fatalf("create preflight template: %d %s", w.Code, w.Body)
	}
	if w := h.do("POST", "/api/agents", map[string]any{
		"name": "bad-tpl", "templateId": "nope",
	}); w.Code != 400 {
		t.Fatalf("unknown templateId: %d", w.Code)
	}
	// Blank regression: no templateId still works
	if w := h.do("POST", "/api/agents", map[string]any{"name": "blank-ok"}); w.Code != 201 {
		t.Fatalf("blank create: %d %s", w.Code, w.Body)
	}
	// Templates list includes preflight; engineer roster conceptually still 9 via bootstrap
	if w := h.do("GET", "/api/agent-teams/templates", nil); w.Code != 200 {
		t.Fatalf("templates: %d", w.Code)
	} else {
		var body struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		var hasPF, hasTest bool
		for _, it := range body.Items {
			if it.ID == "preflight" {
				hasPF = true
			}
			if it.ID == "test" {
				hasTest = true
			}
		}
		if !hasPF || !hasTest {
			t.Fatalf("templates missing preflight/test: %+v", body.Items)
		}
	}
	if w := h.do("GET", "/api/agents/a1", nil); w.Code != 200 {
		t.Fatalf("get: %d", w.Code)
	}
	if w := h.do("GET", "/api/agents/ghost", nil); w.Code != 404 {
		t.Fatalf("get missing: %d", w.Code)
	}
	// Save (PUT).
	if w := h.do("PUT", "/api/agents/a1", map[string]any{"name": "a1"}); w.Code != 200 {
		t.Fatalf("save: %d", w.Code)
	}
	if w := h.do("PUT", "/api/agents/a1", "bad"); w.Code != 400 {
		t.Fatalf("save bad: %d", w.Code)
	}
	// Rename.
	if w := h.do("POST", "/api/agents/a1/rename", "bad"); w.Code != 400 {
		t.Fatalf("rename bad: %d", w.Code)
	}
	if w := h.do("POST", "/api/agents/a1/rename", map[string]any{"name": ""}); w.Code != 400 {
		t.Fatalf("rename empty: %d", w.Code)
	}
	if w := h.do("POST", "/api/agents/ghost/rename", map[string]any{"name": "z"}); w.Code != 404 {
		t.Fatalf("rename missing: %d", w.Code)
	}
	if w := h.do("POST", "/api/agents/a1/rename", map[string]any{"name": "a2"}); w.Code != 200 {
		t.Fatalf("rename: %d %s", w.Code, w.Body)
	}
	// Delete.
	if w := h.do("DELETE", "/api/agents/a2", nil); w.Code != 200 {
		t.Fatalf("delete: %d", w.Code)
	}
}

func TestSandboxEndpoints(t *testing.T) {
	h := newHarness(t)
	// List (empty).
	if w := h.do("GET", "/api/sandboxes", nil); w.Code != 200 {
		t.Fatalf("list: %d", w.Code)
	}
	// Cleanup.
	if w := h.do("POST", "/api/sandboxes/cleanup", nil); w.Code != 200 {
		t.Fatalf("cleanup: %d", w.Code)
	}
	// Bad id.
	if w := h.do("GET", "/api/sandboxes/abc", nil); w.Code != 400 {
		t.Fatalf("get bad id: %d", w.Code)
	}
	// Missing.
	if w := h.do("GET", "/api/sandboxes/999", nil); w.Code != 404 {
		t.Fatalf("get missing: %d", w.Code)
	}
	if w := h.do("POST", "/api/sandboxes/abc/stop", nil); w.Code != 400 {
		t.Fatalf("stop bad id: %d", w.Code)
	}
	if w := h.do("DELETE", "/api/sandboxes/abc", nil); w.Code != 400 {
		t.Fatalf("destroy bad id: %d", w.Code)
	}
	// Seed a stopped sandbox and exercise stop/destroy/log endpoints.
	h.db.Create(&models.Sandbox{Name: "grasp-sb-h1", Purpose: "test", Status: "stopped"})
	var row models.Sandbox
	h.db.Where("name = ?", "grasp-sb-h1").First(&row)
	if w := h.do("GET", "/api/sandboxes/999/log", nil); w.Code != 200 {
		t.Fatalf("log missing: %d", w.Code)
	}
	// Log endpoint (not found archive -> found:false but 200).
	path := "/api/sandboxes/" + uintToStr(row.ID)
	if w := h.do("GET", path+"/log", nil); w.Code != 200 {
		t.Fatalf("log: %d", w.Code)
	}
	// Events / eventlog: bad id -> 400; live-less sandbox -> 502 bad gateway.
	if w := h.do("GET", "/api/sandboxes/abc/events", nil); w.Code != 400 {
		t.Fatalf("events bad id: %d", w.Code)
	}
	if w := h.do("GET", "/api/sandboxes/abc/eventlog", nil); w.Code != 400 {
		t.Fatalf("eventlog bad id: %d", w.Code)
	}
	if w := h.do("GET", path+"/events", nil); w.Code != 502 {
		t.Fatalf("events no conn: %d", w.Code)
	}
	if w := h.do("GET", path+"/eventlog", nil); w.Code != 502 {
		t.Fatalf("eventlog no conn: %d", w.Code)
	}

	if w := h.do("POST", path+"/stop", nil); w.Code != 200 {
		t.Fatalf("stop: %d %s", w.Code, w.Body)
	}
	if w := h.do("DELETE", path, nil); w.Code != 200 {
		t.Fatalf("destroy: %d", w.Code)
	}
}

func TestSandboxProxyEndpoints(t *testing.T) {
	h := newHarness(t)
	if w := h.do("GET", "/sandbox/abc/", nil); w.Code != 400 {
		t.Fatalf("proxy bad id: %d", w.Code)
	}
	if w := h.do("GET", "/sandbox-bridge/abc/", nil); w.Code != 400 {
		t.Fatalf("bridge proxy bad id: %d", w.Code)
	}
	if w := h.do("GET", "/sandbox-acp/abc/", nil); w.Code != 400 {
		t.Fatalf("acp proxy bad id: %d", w.Code)
	}
	// No code-server / no acp -> 404.
	h.db.Create(&models.Sandbox{Name: "grasp-sb-p1", Purpose: "test", Status: "running"})
	var row models.Sandbox
	h.db.Where("name = ?", "grasp-sb-p1").First(&row)
	if w := h.do("GET", "/sandbox/"+uintToStr(row.ID)+"/", nil); w.Code != 404 {
		t.Fatalf("proxy no cs: %d", w.Code)
	}
	if w := h.do("GET", "/sandbox-bridge/"+uintToStr(row.ID)+"/", nil); w.Code != 404 {
		t.Fatalf("bridge proxy no acp: %d", w.Code)
	}
	if w := h.do("GET", "/sandbox-acp/"+uintToStr(row.ID)+"/", nil); w.Code != 404 {
		t.Fatalf("acp proxy no acp: %d", w.Code)
	}
}

func TestWorkflowInputVariablesLift(t *testing.T) {
	h := newHarness(t)
	// An input node carrying a `variables` block in config exercises the
	// liftInputVariables promotion + the inputs-drop path.
	body := map[string]any{
		"name": "WFV", "projectId": models.DefaultProjectID,
		"nodes": []map[string]any{
			{"id": "in", "type": "input", "config": map[string]any{
				"variables": []map[string]any{{"name": "idea", "type": "string", "ask": true}},
				"inputs":    []map[string]any{{"legacy": true}},
			}},
			{"id": "out", "type": "output"},
		},
		"edges": []map[string]any{{"id": "e", "source": "in", "target": "out"}},
	}
	w := h.do("POST", "/api/workflows", body)
	if w.Code != 200 {
		t.Fatalf("create with vars: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "idea") {
		t.Fatalf("variables not lifted: %s", w.Body)
	}
}

func TestSandboxProxySuccess(t *testing.T) {
	h := newHarness(t)
	// Upstream that both the code-server and acp proxies forward to.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("upstream-ok"))
	}))
	defer upstream.Close()
	upHost := strings.TrimPrefix(upstream.URL, "http://")
	_, portStr, _ := strings.Cut(upHost, ":")
	port, _ := strconv.Atoi(portStr)

	const sbName = "grasp-sb-px"
	h.fg.Seed(sbName)
	h.fg.SetEndpoints(sbName, map[string]string{
		"ide":     upHost,
		"session": upHost,
		"ssh":     "127.0.0.1:2222",
	})
	h.db.Create(&models.Sandbox{Name: sbName, Purpose: "test", Status: "running", CodeServerPort: port, ACPPort: port})
	var row models.Sandbox
	h.db.Where("name = ?", sbName).First(&row)

	// The reverse proxy needs a real ResponseWriter (Flusher/Hijacker), so run
	// against a live test server rather than a recorder.
	srv := httptest.NewServer(h.r)
	defer srv.Close()
	resp, err := h.httpGet(srv.URL + "/sandbox/" + uintToStr(row.ID) + "/foo")
	if err != nil {
		t.Fatalf("code-server proxy: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("code-server proxy status: %d", resp.StatusCode)
	}
	resp2, err := h.httpGet(srv.URL + "/sandbox-bridge/" + uintToStr(row.ID) + "/bar")
	if err != nil {
		t.Fatalf("bridge proxy: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("bridge proxy status: %d", resp2.StatusCode)
	}
	resp3, err := h.httpGet(srv.URL + "/sandbox-acp/" + uintToStr(row.ID) + "/legacy")
	if err != nil {
		t.Fatalf("acp proxy: %v", err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != 200 {
		t.Fatalf("acp proxy status: %d", resp3.StatusCode)
	}
}

func TestSandboxProxyAutoLogin(t *testing.T) {
	h := newHarness(t)
	loginHits := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" && r.Method == http.MethodPost {
			loginHits++
			_ = r.ParseForm()
			if r.Form.Get("password") != "tok-secret" {
				http.Error(w, "bad password", http.StatusUnauthorized)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "key", Value: "sess", Path: "/"})
			w.WriteHeader(http.StatusFound)
			return
		}
		if cookie, err := r.Cookie("key"); err != nil || cookie.Value != "sess" {
			http.Error(w, "need login", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("ide-ok"))
	}))
	defer upstream.Close()
	upHost := strings.TrimPrefix(upstream.URL, "http://")
	_, portStr, _ := strings.Cut(upHost, ":")
	port, _ := strconv.Atoi(portStr)

	const sbName = "grasp-sb-autologin"
	h.fg.Seed(sbName)
	h.fg.SetEndpoints(sbName, map[string]string{
		"ide":     upHost,
		"session": upHost,
		"ssh":     "127.0.0.1:2222",
	})
	h.db.Create(&models.Sandbox{
		Name: sbName, Purpose: "test", Status: "running",
		CodeServerPort: port, ACPPort: port, Token: "tok-secret",
	})
	var row models.Sandbox
	h.db.Where("name = ?", sbName).First(&row)

	srv := httptest.NewServer(h.r)
	defer srv.Close()
	resp, err := h.httpGet(srv.URL + "/sandbox/" + uintToStr(row.ID) + "/")
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if string(body) != "ide-ok" {
		t.Fatalf("body = %q, want ide-ok", body)
	}
	if loginHits != 1 {
		t.Fatalf("login hits = %d, want 1", loginHits)
	}
	found := false
	for _, c := range resp.Cookies() {
		if c.Name == "key" && c.Value == "sess" && strings.HasPrefix(c.Path, "/sandbox/") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing mounted key cookie; got %#v", resp.Cookies())
	}
}

func TestSandboxProxyDialsGatewayHostPort(t *testing.T) {
	h := newHarness(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()
	upHost := strings.TrimPrefix(upstream.URL, "http://")
	_, portStr, _ := strings.Cut(upHost, ":")
	port, _ := strconv.Atoi(portStr)

	// Publish a non-loopback host in gateway endpoints (same port as the real
	// upstream via /etc/hosts is unavailable in CI). Instead assert the
	// service-layer resolver returns the full host:port from endpoints — that
	// is the dial target SandboxProxy/SandboxACPProxy use.
	const sbName = "grasp-sb-nonloop"
	wantIDE := "10.42.9.9:" + portStr
	wantACP := "10.42.9.10:" + portStr
	h.fg.Seed(sbName)
	h.fg.SetEndpoints(sbName, map[string]string{
		"ide":     wantIDE,
		"session": wantACP,
		"ssh":     "10.42.9.9:22",
	})
	h.db.Create(&models.Sandbox{
		Name: sbName, Purpose: "test", Status: "running",
		Host: "10.42.9.10", CodeServerPort: port, ACPPort: port,
	})
	var row models.Sandbox
	h.db.Where("name = ?", sbName).First(&row)

	ctx := context.Background()
	ide, err := h.h.Sbx.IDEUpstream(ctx, row.ID)
	if err != nil {
		t.Fatalf("IDEUpstream: %v", err)
	}
	if ide != wantIDE {
		t.Fatalf("IDEUpstream = %q, want non-loopback %q", ide, wantIDE)
	}
	if strings.HasPrefix(ide, "127.0.0.1:") {
		t.Fatalf("IDEUpstream must not fall back to 127.0.0.1, got %q", ide)
	}
	acp, err := h.h.Sbx.ACPUpstream(ctx, row.ID)
	if err != nil {
		t.Fatalf("ACPUpstream: %v", err)
	}
	if acp != wantACP {
		t.Fatalf("ACPUpstream = %q, want non-loopback %q", acp, wantACP)
	}
	if strings.HasPrefix(acp, "127.0.0.1:") {
		t.Fatalf("ACPUpstream must not fall back to 127.0.0.1, got %q", acp)
	}
}

// closedLocalAddr binds 127.0.0.1:0 then closes so dials fail with
// connection-refused immediately (avoids TEST-NET blackhole i/o waits that
// inflate package wall time under cold/slow runners).
func closedLocalAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func TestSandboxProxyErrorHandler(t *testing.T) {
	h := newHarness(t)
	const sbName = "grasp-sb-unreach"
	// Closed local ports → instant dial failure into ReverseProxy ErrorHandler.
	dialIDE := closedLocalAddr(t)
	dialACP := closedLocalAddr(t)
	h.fg.Seed(sbName)
	h.fg.SetEndpoints(sbName, map[string]string{
		"ide":     dialIDE,
		"session": dialACP,
		"ssh":     "127.0.0.1:1",
	})
	h.db.Create(&models.Sandbox{
		Name: sbName, Purpose: "test", Status: "running",
		Host: "127.0.0.1", CodeServerPort: 1, ACPPort: 1,
	})
	var row models.Sandbox
	h.db.Where("name = ?", sbName).First(&row)

	srv := httptest.NewServer(h.r)
	defer srv.Close()

	assertFriendly := func(t *testing.T, path, channel, dial string) {
		t.Helper()
		resp, err := h.httpGet(srv.URL + path)
		if err != nil {
			t.Fatalf("%s: %v", channel, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("%s status = %d, want 502; body=%s", channel, resp.StatusCode, body)
		}
		s := string(body)
		if !strings.Contains(s, dial) {
			t.Fatalf("%s error page missing dial target %q; body=%s", channel, dial, s)
		}
		if !strings.Contains(s, channel) {
			t.Fatalf("%s error page missing channel label; body=%s", channel, s)
		}
		if !strings.Contains(s, "通道不可达") {
			t.Fatalf("%s error page missing friendly copy; body=%s", channel, s)
		}
	}

	id := uintToStr(row.ID)
	assertFriendly(t, "/sandbox/"+id+"/", "IDE", dialIDE)
	assertFriendly(t, "/sandbox-bridge/"+id+"/", "ACP", dialACP)
	assertFriendly(t, "/sandbox-acp/"+id+"/legacy", "ACP", dialACP)
}

func TestMCPRPCEndpoint(t *testing.T) {
	h := newHarness(t)
	// Unauthorized (no token).
	if w := h.do("POST", "/mcp/runs/run1", map[string]any{}); w.Code != 401 {
		t.Fatalf("mcp unauthorized: %d", w.Code)
	}
	tok := h.host.RegisterRun("run1")
	// GET ack after authorize.
	req := httptest.NewRequest("GET", "/mcp/runs/run1", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("mcp GET ack: %d", w.Code)
	}
	// POST JSON-RPC ping.
	req = httptest.NewRequest("POST", "/mcp/runs/run1", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("mcp POST ping: %d %s", w.Code, w.Body)
	}
}

func TestSPAFallback(t *testing.T) {
	h := newHarness(t)
	// Unknown API path -> 404.
	if w := h.do("GET", "/api/unknown", nil); w.Code != 404 {
		t.Fatalf("api unknown: %d", w.Code)
	}
	// Unknown mcp path -> 404.
	if w := h.do("GET", "/mcp/whatever", nil); w.Code != 404 {
		t.Fatalf("mcp unknown: %d", w.Code)
	}
	// Non-API deep link -> falls to index.html file (may be 404 if absent, but
	// exercises the NoRoute handler branch).
	h.do("GET", "/some/spa/route", nil)
}

func TestListRunsCurrentNodeLabel(t *testing.T) {
	h := newHarness(t)
	// Prevent the FIFO dispatcher from admitting the synthetic "queued" run
	// before we list — otherwise status flips to running and a label appears.
	h.h.Eng.Halt()
	graph := models.Graph{Nodes: []models.Node{
		{ID: "impl", Label: "实现"},
		{ID: "gate", Label: "方案评审门禁"},
		{ID: "react", Label: "澄清"},
		{ID: "blank", Label: "   "},
	}}
	h.db.Create(&models.Run{ID: "run-running", Status: "running", WorkflowID: "wf-1", Graph: graph})
	h.db.Create(&models.Run{ID: "run-gate", Status: "waiting_human", WorkflowID: "wf-1", Graph: graph})
	h.db.Create(&models.Run{ID: "run-react", Status: "waiting_human", WorkflowID: "wf-1", Graph: graph})
	h.db.Create(&models.Run{ID: "run-blank", Status: "running", WorkflowID: "wf-1", Graph: graph})
	h.db.Create(&models.Run{ID: "run-sandbox-fail", Status: "running", WorkflowID: "wf-1", Graph: graph})
	h.db.Create(&models.Run{ID: "run-done", Status: "completed", WorkflowID: "wf-1", Graph: graph})
	h.db.Create(&models.Run{ID: "run-queued", Status: "queued", WorkflowID: "wf-1", Graph: graph})

	h.db.Create(&models.StateRun{RunID: "run-running", NodeID: "impl", Iteration: 1, Status: "running"})
	h.db.Create(&models.Gate{RunID: "run-gate", NodeID: "gate", Title: "审批"})
	h.db.Create(&models.StateRun{RunID: "run-react", NodeID: "react", Iteration: 1, Status: "waiting_human"})
	h.db.Create(&models.StateRun{RunID: "run-blank", NodeID: "blank", Iteration: 1, Status: "running"})
	h.db.Create(&models.StateRun{RunID: "run-sandbox-fail", NodeID: "impl", Iteration: 1, Status: "failed",
		Error: "sandbox setup failed: docker pull failed"})

	w := h.do("GET", "/api/runs", nil)
	if w.Code != 200 {
		t.Fatalf("list runs: %d %s", w.Code, w.Body)
	}
	var rows []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := map[string]map[string]any{}
	for _, row := range rows {
		byID[row["id"].(string)] = row
	}

	assertLabel := func(id, want string) {
		t.Helper()
		row, ok := byID[id]
		if !ok {
			t.Fatalf("missing run %s", id)
		}
		got, ok := row["currentNodeLabel"].(string)
		if want == "" {
			if ok && got != "" {
				t.Fatalf("%s: want no label, got %q", id, got)
			}
			return
		}
		if !ok || got != want {
			t.Fatalf("%s: label = %q, want %q", id, got, want)
		}
	}

	assertLabel("run-running", "实现")
	assertLabel("run-gate", "方案评审门禁")
	assertLabel("run-react", "澄清")
	assertLabel("run-blank", "")
	assertLabel("run-sandbox-fail", "")
	assertLabel("run-done", "")
	assertLabel("run-queued", "")
}

func TestListRunsStatusFilter(t *testing.T) {
	h := newHarness(t)
	base := time.Date(2026, 7, 4, 18, 30, 0, 0, time.UTC)
	h.db.Create(&models.Run{ID: "run-a", Status: "running", WorkflowID: "wf-1", StartedAt: base.Add(12 * time.Second), CreatedAt: base})
	h.db.Create(&models.Run{ID: "run-b", Status: "completed", WorkflowID: "wf-1", StartedAt: base.Add(10 * time.Second), CreatedAt: base})
	h.db.Create(&models.Run{ID: "run-c", Status: "failed", WorkflowID: "wf-2", StartedAt: base.Add(8 * time.Second), CreatedAt: base})

	// Single value backward compat.
	w := h.do("GET", "/api/runs?status=running", nil)
	if w.Code != 200 {
		t.Fatalf("single status: %d", w.Code)
	}
	var single []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &single); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(single) != 1 || single[0]["id"] != "run-a" {
		t.Fatalf("single status filter: got %v", single)
	}

	// Multi-value OR.
	w = h.do("GET", "/api/runs?status=running,failed", nil)
	if w.Code != 200 {
		t.Fatalf("multi status: %d", w.Code)
	}
	var multi []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &multi); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(multi) != 2 {
		t.Fatalf("multi status OR: want 2, got %d", len(multi))
	}

	// Invalid values stripped; all invalid → no filter.
	w = h.do("GET", "/api/runs?status=running,bogus", nil)
	json.Unmarshal(w.Body.Bytes(), &multi)
	if len(multi) != 1 {
		t.Fatalf("partial invalid: want 1, got %d", len(multi))
	}
	w = h.do("GET", "/api/runs?status=bogus,unknown", nil)
	json.Unmarshal(w.Body.Bytes(), &multi)
	if len(multi) != 3 {
		t.Fatalf("all invalid should return all runs: got %d", len(multi))
	}

	// Multi status AND wf.
	w = h.do("GET", "/api/runs?status=running,failed&wf=wf-2", nil)
	json.Unmarshal(w.Body.Bytes(), &multi)
	if len(multi) != 1 || multi[0]["id"] != "run-c" {
		t.Fatalf("multi AND wf: got %v", multi)
	}
}

func uintToStr(u uint) string {
	return strconv.FormatUint(uint64(u), 10)
}

// TestRunDetailRichBranches exercises runDetailDTO's git / gate / clarify
// branches and the live effectiveRunDuration path.
func TestRunDetailRichBranches(t *testing.T) {
	h := newHarness(t)
	h.db.Create(&models.Run{ID: "rd", Status: "waiting_human", StartedAt: time.Now().Add(-time.Minute), Graph: models.Graph{
		Nodes: []models.Node{{ID: "impl", Type: "implement"}, {ID: "gate", Type: "human_gate"}},
	}})
	// A node execution carrying git push info -> git block.
	h.db.Create(&models.StateRun{RunID: "rd", NodeID: "impl", Iteration: 1, Status: "completed",
		Outputs: map[string]any{"pushed_sha": "abc123", "branch": "feature/x", "mr_url": "http://mr/1"}})
	// Failed node with error text -> nodeRuns.error.
	h.db.Create(&models.StateRun{RunID: "rd", NodeID: "gate", Iteration: 1, Status: "failed",
		Error: "sandbox setup failed: docker pull registry.example/sandbox:latest: exit status 1"})
	// A pending gate -> gate block.
	h.db.Create(&models.Gate{RunID: "rd", NodeID: "gate", Title: "审批", RequestedAt: time.Now()})
	// A react conversation -> clarify + clarifyByNode blocks.
	h.db.Create(&models.ReactConversation{RunID: "rd", NodeID: "impl", Iteration: 1,
		PreviewArtifact: "page.html",
		Messages:        []models.ReactMessage{{Role: "human", Text: "hi"}}})

	w := h.do("GET", "/api/runs/rd", nil)
	if w.Code != 200 {
		t.Fatalf("get run: %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{`"git"`, "abc123", `"gate"`, `"clarify"`, `"clarifyByNode"`, "docker pull registry.example", `"previewArtifact":"page.html"`} {
		if !strings.Contains(body, want) {
			t.Errorf("run detail missing %q", want)
		}
	}
	// Artifacts must be metadata-only (no inlined content).
	h.db.Create(&models.Artifact{
		ID: "art-rd", RunID: "rd", NodeID: "impl", Name: "plan.json", Kind: "json",
		Content: `{"secret":"big-body"}`, SizeBytes: 99, Revision: 2,
	})
	w = h.do("GET", "/api/runs/rd", nil)
	if w.Code != 200 {
		t.Fatalf("get run with artifact: %d", w.Code)
	}
	body = w.Body.String()
	if strings.Contains(body, `"secret"`) || strings.Contains(body, "big-body") {
		t.Error("run detail artifacts must not inline content")
	}
	if !strings.Contains(body, `"artifacts"`) || !strings.Contains(body, "plan.json") {
		t.Error("run detail should still list artifact metadata")
	}
	if !strings.Contains(body, `"revision":2`) {
		t.Error("run detail artifacts should include revision")
	}
}

// TestRunDetailFailedExposesRunLevelError ensures failed runs lift a human
// reason to the detail DTO (banner / API) without requiring a node click.
func TestRunDetailFailedExposesRunLevelError(t *testing.T) {
	h := newHarness(t)
	h.db.Create(&models.Run{ID: "rd-fail", Status: "failed", StartedAt: time.Now().Add(-time.Minute), Graph: models.Graph{
		Nodes: []models.Node{{ID: "research", Type: "research"}, {ID: "out", Type: "output"}},
	}})
	h.db.Create(&models.StateRun{RunID: "rd-fail", NodeID: "research", Iteration: 1, Status: "failed",
		Error: "sandbox setup failed: create timeout"})

	w := h.do("GET", "/api/runs/rd-fail", nil)
	if w.Code != 200 {
		t.Fatalf("get run: %d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	for _, want := range []string{`"error"`, `"failedReason"`, "sandbox setup failed", `"failedNode":"research"`} {
		if !strings.Contains(body, want) {
			t.Errorf("failed run detail missing %q in %s", want, body)
		}
	}

	// Completed runs must not carry misleading failure fields.
	h.db.Create(&models.Run{ID: "rd-ok", Status: "completed", Progress: 1, StartedAt: time.Now().Add(-time.Minute), Graph: models.Graph{
		Nodes: []models.Node{{ID: "out", Type: "output"}},
	}})
	w = h.do("GET", "/api/runs/rd-ok", nil)
	if w.Code != 200 {
		t.Fatalf("get ok run: %d", w.Code)
	}
	okBody := w.Body.String()
	if strings.Contains(okBody, `"failedReason"`) || strings.Contains(okBody, `"noSandboxLog"`) {
		t.Fatalf("completed run must omit failure fields: %s", okBody)
	}
}

// TestSandboxLogFoundBranches drives the "found" path of the sandbox log
// endpoints by seeding an archived log alongside a stopped sandbox row.
func TestSandboxLogFoundBranches(t *testing.T) {
	h := newHarness(t)
	row := &models.Sandbox{Name: "grasp-sb-logh", Purpose: "run", Status: "exited", RunID: "runH", NodeID: "n1"}
	h.db.Create(row)
	h.db.Create(&models.SandboxLog{Name: row.Name, RunID: "runH", NodeID: "n1", Content: "archived"})

	// By id.
	w := h.do("GET", "/api/sandboxes/"+uintToStr(row.ID)+"/log", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"found":true`) {
		t.Fatalf("sandbox log by id: %d %s", w.Code, w.Body)
	}
	// By run node.
	w = h.do("GET", "/api/runs/runH/nodes/n1/sandbox-log", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"found":true`) {
		t.Fatalf("node sandbox log: %d %s", w.Code, w.Body)
	}
	// GetSandbox view + Events (dead port -> error) exercise more branches.
	if w := h.do("GET", "/api/sandboxes/"+uintToStr(row.ID), nil); w.Code != 200 {
		t.Fatalf("get sandbox: %d", w.Code)
	}
}

// TestSandboxLogLiveErrorSurfaced ensures a live logs read failure returns an
// error field (found=false) rather than the empty "no source" disguise.
func TestSandboxLogLiveErrorSurfaced(t *testing.T) {
	h := newHarness(t)
	row := &models.Sandbox{Name: "grasp-sb-logerr", Purpose: "run", Status: "running", RunID: "runE", NodeID: "n1"}
	h.db.Create(row)
	h.fg.SetStatus(row.Name, "running")
	h.fg.FailLogs = true

	w := h.do("GET", "/api/runs/runE/nodes/n1/sandbox-log", nil)
	if w.Code != 200 {
		t.Fatalf("code=%d body=%s", w.Code, w.Body)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"found":false`) || !strings.Contains(body, `"error"`) {
		t.Fatalf("want found=false with error, got %s", body)
	}
	if strings.Contains(body, "暂无沙箱日志") {
		t.Fatal("error must not use empty-state copy")
	}
}

// TestHandlerDBErrorBranches closes the DB connection and exercises the
// error/500 branches of the read/delete handlers that talk to the database.
func TestHandlerDBErrorBranches(t *testing.T) {
	h := newHarness(t)
	sqlDB, err := h.db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.Close() // subsequent queries fail

	// Exercise handlers against a dead DB; they must degrade to an error status
	// (or an empty-but-OK response for the ones that swallow read errors) rather
	// than panic. DeleteWorkflow propagates the DB error as a 500.
	if w := h.do("DELETE", "/api/workflows/x", nil); w.Code < 400 {
		t.Errorf("delete workflow on dead db: expected error status, got %d", w.Code)
	}
	// These just need to not panic (read handlers swallow errors).
	h.do("GET", "/api/workflows", nil)
	h.do("GET", "/api/sandboxes", nil)
	h.do("GET", "/api/runs", nil)
	h.do("GET", "/api/stats/dashboard", nil)
}

// TestAgentErrorBranches covers the 400 validation branches of the agent
// save/rename handlers.
func TestAgentErrorBranches(t *testing.T) {
	h := newHarness(t)
	// Seed two agents to rename around.
	_ = h.h.Agents.Save(services.Agent{Name: "keep"})
	_ = h.h.Agents.Save(services.Agent{Name: "taken"})

	// SaveAgent with a malformed body -> 400.
	if w := h.do("PUT", "/api/agents/keep", "not-json"); w.Code != 400 {
		t.Errorf("save bad body: %d", w.Code)
	}
	// RenameAgent malformed body -> 400.
	if w := h.do("POST", "/api/agents/keep/rename", "not-json"); w.Code != 400 {
		t.Errorf("rename bad body: %d", w.Code)
	}
	// RenameAgent empty name -> 400.
	if w := h.do("POST", "/api/agents/keep/rename", map[string]any{"name": "  "}); w.Code != 400 {
		t.Errorf("rename empty: %d", w.Code)
	}
	// RenameAgent onto an existing name -> conflict/error.
	if w := h.do("POST", "/api/agents/keep/rename", map[string]any{"name": "taken"}); w.Code < 400 {
		t.Errorf("rename conflict: %d", w.Code)
	}
}

func TestListRunsPagination(t *testing.T) {
	h := newHarness(t)
	base := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		h.db.Create(&models.Run{
			ID: "run-pg-" + strconv.Itoa(i), Status: "completed", WorkflowID: "wf-1",
			StartedAt: base.Add(time.Duration(i) * time.Second), CreatedAt: base,
		})
	}

	// Bare array without pagination params.
	w := h.do("GET", "/api/runs", nil)
	if w.Code != 200 {
		t.Fatalf("bare list: %d", w.Code)
	}
	var bare []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &bare); err != nil {
		t.Fatalf("decode bare: %v", err)
	}
	if len(bare) != 5 {
		t.Fatalf("bare list want 5, got %d", len(bare))
	}

	// Paginated wrapper.
	w = h.do("GET", "/api/runs?page=1&pageSize=2", nil)
	if w.Code != 200 {
		t.Fatalf("paged list: %d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode paged: %v", err)
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("page items want 2, got %d", len(items))
	}
	if int(body["total"].(float64)) != 5 {
		t.Fatalf("total want 5, got %v", body["total"])
	}
	if body["hasMore"] != true {
		t.Fatal("hasMore want true on page 1")
	}

	// pageSize over limit -> 400.
	w = h.do("GET", "/api/runs?page=1&pageSize=101", nil)
	if w.Code != 400 {
		t.Fatalf("oversize pageSize: %d %s", w.Code, w.Body)
	}
}

func TestListRunsSortParams(t *testing.T) {
	h := newHarness(t)
	base := time.Date(2026, 7, 9, 10, 0, 0, 0, time.UTC)
	h.db.Create(&models.Run{
		ID: "run-hi", Status: "completed", WorkflowID: "wf-1",
		Priority: models.PriorityHigh, StartedAt: base, CreatedAt: base,
	})
	h.db.Create(&models.Run{
		ID: "run-lo", Status: "completed", WorkflowID: "wf-1",
		Priority: models.PriorityLow, StartedAt: base.Add(time.Second), CreatedAt: base,
	})

	w := h.do("GET", "/api/runs?page=1&pageSize=10&sort=priority&order=desc", nil)
	if w.Code != 200 {
		t.Fatalf("priority desc: %d %s", w.Code, w.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	items := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if items[0].(map[string]any)["id"] != "run-hi" {
		t.Fatalf("priority desc first want run-hi, got %v", items[0])
	}

	// Illegal sort falls back to default hybrid-time DESC (run-lo started later).
	w = h.do("GET", "/api/runs?page=1&pageSize=10&sort=duration&order=desc", nil)
	if w.Code != 200 {
		t.Fatalf("illegal sort: %d %s", w.Code, w.Body)
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	items = body["items"].([]any)
	if items[0].(map[string]any)["id"] != "run-lo" {
		t.Fatalf("illegal sort fallback first want run-lo, got %v", items[0])
	}
}

func TestListArtifactsPaginationAndWf(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{ID: "run-art", WorkflowID: "wf-a", WorkflowName: "A", Title: "Plan Sprint"})
	h.db.Create(&models.Run{ID: "run-art2", WorkflowID: "wf-b", WorkflowName: "B", Title: "Other Run"})
	h.db.Create(&models.Run{ID: "run-u", WorkflowID: "", WorkflowName: "", Title: "Loose Run"})
	h.db.Create(&models.Artifact{ID: "art-1", RunID: "run-art", WorkflowID: "wf-a", NodeID: "plan", Name: "plan.json", CreatedAt: now})
	h.db.Create(&models.Artifact{ID: "art-2", RunID: "run-art2", WorkflowID: "wf-b", NodeID: "research", Name: "b.json", CreatedAt: now.Add(time.Second)})
	h.db.Create(&models.Artifact{ID: "art-u", RunID: "run-u", WorkflowID: "", NodeID: "implement", Name: "unnamed.json", CreatedAt: now.Add(2 * time.Second)})

	w := h.do("GET", "/api/artifacts?page=1&pageSize=1&wf=wf-a", nil)
	if w.Code != 200 {
		t.Fatalf("paged artifacts: %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("wf filter want 1 item, got %d", len(items))
	}
	if int(body["total"].(float64)) != 1 {
		t.Fatalf("wf total want 1, got %v", body["total"])
	}

	w = h.do("GET", "/api/artifacts?page=1&pageSize=20&wf=wf-a&q=plan", nil)
	if w.Code != 200 {
		t.Fatalf("q search: %d", w.Code)
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	items = body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("q search want 1 item, got %d", len(items))
	}
	first := items[0].(map[string]any)
	if first["name"] != "plan.json" {
		t.Fatalf("q search name want plan.json, got %v", first["name"])
	}

	w = h.do("GET", "/api/artifacts?page=1&pageSize=20&wf=wf-a&q=Sprint", nil)
	if w.Code != 200 {
		t.Fatalf("q run title search: %d", w.Code)
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if int(body["total"].(float64)) != 1 {
		t.Fatalf("q run title total want 1, got %v", body["total"])
	}

	w = h.do("GET", "/api/artifacts?page=1&pageSize=20&wf=__unnamed__", nil)
	if w.Code != 200 {
		t.Fatalf("unnamed wf: %d", w.Code)
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	items = body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("__unnamed__ want 1 item, got %d", len(items))
	}
	first = items[0].(map[string]any)
	if first["name"] != "unnamed.json" {
		t.Fatalf("__unnamed__ name want unnamed.json, got %v", first["name"])
	}

	w = h.do("GET", "/api/artifacts?page=1&pageSize=20&wf=wf-a&q=zzznomatch999", nil)
	if w.Code != 200 {
		t.Fatalf("q no match: %d", w.Code)
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["items"] == nil {
		t.Fatal("q no match items must be [] not null")
	}
	items = body["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("q no match want 0 items, got %d", len(items))
	}
	if int(body["total"].(float64)) != 0 {
		t.Fatalf("q no match total want 0, got %v", body["total"])
	}
}

func TestListArtifactsGroupByRun(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{ID: "run-a", WorkflowID: "wf-a", WorkflowName: "A", Title: "Run A"})
	h.db.Create(&models.Run{ID: "run-b", WorkflowID: "wf-a", WorkflowName: "A", Title: "Run B"})
	// run-a: two artifacts; newer than run-b's single artifact.
	h.db.Create(&models.Artifact{ID: "art-a1", RunID: "run-a", WorkflowID: "wf-a", NodeID: "plan", Name: "plan.json", CreatedAt: now.Add(-time.Minute)})
	h.db.Create(&models.Artifact{ID: "art-a2", RunID: "run-a", WorkflowID: "wf-a", NodeID: "research", Name: "research.json", CreatedAt: now})
	h.db.Create(&models.Artifact{ID: "art-b1", RunID: "run-b", WorkflowID: "wf-a", NodeID: "plan", Name: "other.json", CreatedAt: now.Add(-time.Hour)})

	// Default path (no groupBy): pageSize=1 still means 1 artifact row.
	w := h.do("GET", "/api/artifacts?page=1&pageSize=1&wf=wf-a", nil)
	if w.Code != 200 {
		t.Fatalf("default page: %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if int(body["total"].(float64)) != 3 {
		t.Fatalf("default total want 3 artifacts, got %v", body["total"])
	}
	if len(body["items"].([]any)) != 1 {
		t.Fatalf("default pageSize=1 want 1 artifact, got %d", len(body["items"].([]any)))
	}

	// groupBy=run: pageSize=1 → 1 Run, but whole-Run expands to 2 items for run-a.
	w = h.do("GET", "/api/artifacts?page=1&pageSize=1&wf=wf-a&groupBy=run", nil)
	if w.Code != 200 {
		t.Fatalf("groupBy=run: %d %s", w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &body)
	if int(body["total"].(float64)) != 2 {
		t.Fatalf("groupBy=run total want 2 Runs, got %v", body["total"])
	}
	items := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("groupBy=run page1 want 2 arts (whole run-a), got %d", len(items))
	}
	for _, it := range items {
		if it.(map[string]any)["runId"] != "run-a" {
			t.Fatalf("page1 should only be run-a, got %v", it)
		}
	}

	w = h.do("GET", "/api/artifacts?page=2&pageSize=1&wf=wf-a&groupBy=run", nil)
	json.Unmarshal(w.Body.Bytes(), &body)
	items = body["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["runId"] != "run-b" {
		t.Fatalf("page2 want run-b only, got %v", items)
	}

	// Search hit research.json → whole run-a (plan.json included); total=1 Run.
	w = h.do("GET", "/api/artifacts?page=1&pageSize=20&wf=wf-a&groupBy=run&q=research", nil)
	json.Unmarshal(w.Body.Bytes(), &body)
	if int(body["total"].(float64)) != 1 {
		t.Fatalf("search Run total want 1, got %v", body["total"])
	}
	items = body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("search whole-Run want 2 items, got %d", len(items))
	}
}

func TestListGatesPagination(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	h.db.Create(&models.Run{ID: "run-g1", Status: "waiting_human", WorkflowID: "wf-1", StartedAt: now})
	h.db.Create(&models.Gate{RunID: "run-g1", NodeID: "gate1", Title: "G1", RequestedAt: now})
	h.db.Create(&models.StateRun{RunID: "run-g1", NodeID: "gate1", Iteration: 0, Status: "waiting_human"})
	h.db.Create(&models.Run{ID: "run-g2", Status: "waiting_human", WorkflowID: "wf-1", StartedAt: now})
	h.db.Create(&models.Gate{RunID: "run-g2", NodeID: "gate2", Title: "G2", RequestedAt: now.Add(time.Second)})
	h.db.Create(&models.StateRun{RunID: "run-g2", NodeID: "gate2", Iteration: 0, Status: "waiting_human"})

	w := h.do("GET", "/api/gates?page=1&pageSize=1", nil)
	if w.Code != 200 {
		t.Fatalf("paged gates: %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if int(body["total"].(float64)) != 2 {
		t.Fatalf("gates total want 2, got %v", body["total"])
	}
}

func TestNodeEventsPaginationPersisted(t *testing.T) {
	h := newHarness(t)
	events := make([]models.AcpEvent, 25)
	for i := range events {
		events[i] = models.AcpEvent{T: i, Kind: "message", Text: "e"}
	}
	h.db.Create(&models.Run{ID: "run-ev", Status: "completed", StartedAt: time.Now()})
	h.db.Create(&models.StateRun{RunID: "run-ev", NodeID: "n1", Iteration: 1, Status: "completed", Events: events})

	w := h.do("GET", "/api/runs/run-ev/nodes/n1/events?limit=20", nil)
	if w.Code != 200 {
		t.Fatalf("paged events: %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	evs := body["events"].([]any)
	if len(evs) != 20 {
		t.Fatalf("want 20 events, got %d", len(evs))
	}
	if body["hasMore"] != true {
		t.Fatal("hasMore want true")
	}

	w = h.do("GET", "/api/runs/run-ev/nodes/n1/events?limit=101", nil)
	if w.Code != 400 {
		t.Fatalf("oversize limit: %d", w.Code)
	}
}

// liveReadFailProvider simulates a registered live sandbox whose bridge read
// fails — NodeEvents must surface HTTP 200 {unavailable, error, live:false},
// not a fake empty page and not 502 (avoids DevTools Failed to load resource).
type liveReadFailProvider struct{ fakeProvider }

func (liveReadFailProvider) LiveNodeEvents(ctx context.Context, runID, nodeID string) ([]models.AcpEvent, bool, error) {
	return nil, false, errors.New("bridge unreachable")
}

func (liveReadFailProvider) LiveNodeEventsPage(ctx context.Context, runID, nodeID, cursor string, limit int) ([]models.AcpEvent, string, bool, bool, error) {
	return nil, "", false, false, errors.New("bridge unreachable")
}

func TestNodeEventsLiveReadFailureReturnsSoftFail(t *testing.T) {
	h := newHarness(t)
	h.db.Create(&models.Run{ID: "run-live-fail", Status: "running", StartedAt: time.Now()})

	old := h.h.Eng
	eng := engine.New(h.db, liveReadFailProvider{}, h.host, h.h.Arts, 5)
	h.h.Eng = eng
	t.Cleanup(func() {
		eng.Close()
		h.h.Eng = old
	})

	assertSoftFail := func(path string, paginated bool) {
		t.Helper()
		w := h.do(http.MethodGet, path, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200; body=%s", path, w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s json: %v body=%s", path, err, w.Body.String())
		}
		if body["error"] == nil || body["error"] == "" {
			t.Fatalf("%s missing error: %v", path, body)
		}
		if body["live"] != false {
			t.Fatalf("%s live = %v, want false", path, body["live"])
		}
		if body["unavailable"] != true {
			t.Fatalf("%s unavailable = %v, want true", path, body["unavailable"])
		}
		if paginated {
			if _, ok := body["nextCursor"]; !ok {
				t.Fatalf("%s missing nextCursor: %v", path, body)
			}
			if _, ok := body["hasMore"]; !ok {
				t.Fatalf("%s missing hasMore: %v", path, body)
			}
		}
	}

	assertSoftFail("/api/runs/run-live-fail/nodes/n1/events", false)
	assertSoftFail("/api/runs/run-live-fail/nodes/n1/events?limit=20", true)
}

func TestDoctorArtifactSessionIsLoopbackAndTokenProtected(t *testing.T) {
	t.Setenv("GRASP_DOCTOR_TOKEN", "doctor-secret")
	h := newHarness(t)

	request := func(method, path string, headers map[string]string, remoteAddr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = remoteAddr
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		w := httptest.NewRecorder()
		h.r.ServeHTTP(w, req)
		return w
	}

	w := request(http.MethodPost, "/_internal/doctor/artifact-sessions",
		map[string]string{"Authorization": "Bearer doctor-secret"}, "192.0.2.1:1234")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("non-loopback status = %d", w.Code)
	}

	w = request(http.MethodPost, "/_internal/doctor/artifact-sessions",
		map[string]string{"Authorization": "Bearer doctor-secret"}, "127.0.0.1:1234")
	if w.Code != http.StatusCreated {
		t.Fatalf("start status = %d, body=%s", w.Code, w.Body.String())
	}
	var session map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session["id"] == "" || session["run_a"] == "" || session["token_a"] == "" || session["cleanup_token"] == "" {
		t.Fatalf("incomplete session: %#v", session)
	}

	w = request(http.MethodDelete, "/_internal/doctor/artifact-sessions/"+session["id"], map[string]string{
		"Authorization":              "Bearer doctor-secret",
		"X-Grasp-Doctor-Cleanup": session["cleanup_token"],
	}, "127.0.0.1:1234")
	if w.Code != http.StatusNoContent {
		t.Fatalf("cleanup status = %d, body=%s", w.Code, w.Body.String())
	}
}
