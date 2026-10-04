package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/pmmcp"
	"github.com/cocofhu/grasp/internal/services"
)

func TestPmLeaderBindingMemoryAndThreadGate(t *testing.T) {
	hn := newHarness(t)
	enableAdmin(t)

	pm := services.NewPmService(hn.db, hn.h.Agents)
	progress := services.NewPmProgress(pm, hn.h.Runs, hn.h.Arts)
	hn.h.Pm = pm
	hn.h.PmProgress = progress
	hn.h.PMMCP = pmmcp.NewHost(pm, progress, nil, hn.h.Runs, hn.h.Arts, nil)

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "PMProj"})
	if w.Code != 200 {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid := proj["id"].(string)

	w = hn.do(http.MethodPut, "/api/projects/"+pid+"/pm-leader", map[string]any{
		"enabled": true, "agentConfigRef": "no-such-agent",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("enable missing agent: %d %s", w.Code, w.Body.String())
	}

	w = hn.do(http.MethodPut, "/api/projects/"+pid+"/pm-leader", map[string]any{
		"enabled": false, "agentConfigRef": "",
	})
	if w.Code != 200 {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}

	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm-leader", nil)
	if w.Code != 200 {
		t.Fatalf("get binding: %d", w.Code)
	}

	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/memories", map[string]any{
		"title": "背景", "content": "Go 项目",
	})
	if w.Code != 200 {
		t.Fatalf("upsert memory: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/memories", nil)
	if w.Code != 200 {
		t.Fatalf("list memories: %d", w.Code)
	}

	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads", map[string]any{})
	if w.Code != http.StatusConflict {
		t.Fatalf("create thread while disabled want 409 got %d %s", w.Code, w.Body.String())
	}

	if err := hn.h.Agents.Save(services.Agent{Name: "pm-demo", ProjectID: pid}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	w = hn.do(http.MethodPut, "/api/projects/"+pid+"/pm-leader", map[string]any{
		"enabled": true, "agentConfigRef": "pm-demo",
	})
	if w.Code != 200 {
		t.Fatalf("enable: %d %s", w.Code, w.Body.String())
	}

	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads", map[string]any{"title": "问进度"})
	if w.Code != 200 {
		t.Fatalf("create thread: %d %s", w.Code, w.Body.String())
	}
	var thr map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &thr)
	tid := thr["id"].(string)

	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages", map[string]any{
		"role": "user", "content": "整体进度如何？",
	})
	if w.Code != 200 {
		t.Fatalf("append message: %d %s", w.Code, w.Body.String())
	}

	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages", nil)
	if w.Code != 200 {
		t.Fatalf("list messages: %d", w.Code)
	}
	var msgResp struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &msgResp)
	if len(msgResp.Items) != 1 {
		t.Fatalf("messages=%d", len(msgResp.Items))
	}
	mid, _ := msgResp.Items[0]["id"].(string)
	if mid == "" {
		t.Fatal("missing message id")
	}
	if st, _ := msgResp.Items[0]["status"].(string); st != "ok" && st != "" {
		t.Fatalf("default status=%v", msgResp.Items[0]["status"])
	}

	w = hn.do(http.MethodPatch, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages/"+mid, map[string]any{
		"status": "failed", "failKind": "connection",
	})
	if w.Code != 200 {
		t.Fatalf("patch fail: %d %s", w.Code, w.Body.String())
	}
	var patched map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &patched)
	if patched["status"] != "failed" || patched["failKind"] != "connection" {
		t.Fatalf("patched=%v", patched)
	}

	w = hn.do(http.MethodPatch, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages/"+mid, map[string]any{
		"status": "ok",
	})
	if w.Code != 200 {
		t.Fatalf("patch clear: %d %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &patched)
	if patched["status"] != "ok" {
		t.Fatalf("cleared status=%v", patched["status"])
	}

	w = hn.do(http.MethodDelete, "/api/projects/"+pid+"/pm/memories", nil)
	if w.Code != 200 {
		t.Fatalf("clear memories: %d %s", w.Code, w.Body.String())
	}
}

func setupPmTurnThread(t *testing.T, hn *harness, name string) (pm *services.PmService, pid, tid string) {
	t.Helper()
	enableAdmin(t)
	pm = services.NewPmService(hn.db, hn.h.Agents)
	hn.h.Pm = pm
	hn.h.PmProgress = services.NewPmProgress(pm, hn.h.Runs, hn.h.Arts)
	hn.h.PMMCP = pmmcp.NewHost(pm, hn.h.PmProgress, nil, hn.h.Runs, hn.h.Arts, nil)

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": name})
	if w.Code != 200 {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid = proj["id"].(string)
	if err := hn.h.Agents.Save(services.Agent{Name: "pm-" + name, ProjectID: pid}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	w = hn.do(http.MethodPut, "/api/projects/"+pid+"/pm-leader", map[string]any{
		"enabled": true, "agentConfigRef": "pm-" + name,
	})
	if w.Code != 200 {
		t.Fatalf("enable: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads", map[string]any{"title": "t"})
	if w.Code != 200 {
		t.Fatalf("thread: %d %s", w.Code, w.Body.String())
	}
	var thr map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &thr)
	return pm, pid, thr["id"].(string)
}

func TestStartPmTurnValidatesAndMarksRejected(t *testing.T) {
	hn := newHarness(t)
	pm, pid, tid := setupPmTurnThread(t, hn, "TurnReject")
	hn.h.PmTurns = services.NewPmTurnRunner(pm, nil)
	base := "/api/projects/" + pid + "/pm/threads/" + tid

	if w := hn.do(http.MethodPost, base+"/turns", map[string]any{"content": "  "}); w.Code != http.StatusBadRequest {
		t.Fatalf("empty content: %d %s", w.Code, w.Body.String())
	}
	asst, err := pm.AppendMessage(tid, "assistant", "答", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if w := hn.do(http.MethodPost, base+"/turns", map[string]any{"retryOf": asst.ID}); w.Code != http.StatusBadRequest {
		t.Fatalf("retry assistant: %d %s", w.Code, w.Body.String())
	}
	if w := hn.do(http.MethodPost, base+"/turns", map[string]any{"retryOf": "missing"}); w.Code == http.StatusOK {
		t.Fatalf("retry missing: %d", w.Code)
	}

	// No sandbox chat backend: the turn is rejected and the new user message
	// is marked failed instead of being left as an orphan.
	w := hn.do(http.MethodPost, base+"/turns", map[string]any{"content": "进度？"})
	if w.Code != http.StatusConflict {
		t.Fatalf("enqueue: %d %s", w.Code, w.Body.String())
	}
	msgs, _ := pm.ListMessages(tid)
	last := msgs[len(msgs)-1]
	if last.Role != "user" || last.Status != "failed" || last.FailKind != services.PmFailUnknown {
		t.Fatalf("rejected msg=%+v", last)
	}

	if w := hn.do(http.MethodPost, base+"/turns/cancel", nil); w.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
	}
}

type stubPmChat struct{}

func (stubPmChat) ChatWithTimeout(context.Context, uint, string, []models.PromptImage, time.Duration, func(json.RawMessage)) (*models.TokenUsage, models.TokenUsageByModel, error) {
	return nil, nil, errors.New("unreachable")
}

func (stubPmChat) Cancel(uint) {}

func TestStartPmTurnQueuesAndRetry(t *testing.T) {
	hn := newHarness(t)
	pm, pid, tid := setupPmTurnThread(t, hn, "TurnQueue")
	runner := services.NewPmTurnRunner(pm, nil)
	runner.SetChatterForTest(stubPmChat{})
	hn.h.PmTurns = runner
	base := "/api/projects/" + pid + "/pm/threads/" + tid

	w := hn.do(http.MethodPost, base+"/turns", map[string]any{"content": "进度？"})
	if w.Code != http.StatusOK {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Message models.ChatMessage `json:"message"`
		Waiting int                `json:"waiting"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Message.ID == "" || resp.Message.Content != "进度？" || resp.Waiting != 1 {
		t.Fatalf("resp=%+v", resp)
	}
	failed := waitPmMessageFailed(t, pm, tid, resp.Message.ID)
	if failed.FailKind == services.PmFailConnection || failed.FailKind == "" {
		t.Fatalf("server must record a real failKind, got %+v", failed)
	}

	w = hn.do(http.MethodPost, base+"/turns", map[string]any{"retryOf": resp.Message.ID})
	if w.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Message.Status == "failed" {
		t.Fatalf("retry must clear failure, got %+v", resp.Message)
	}
	waitPmMessageFailed(t, pm, tid, resp.Message.ID)
	if msgs, _ := pm.ListMessages(tid); len(msgs) != 1 {
		t.Fatalf("retry must not append a user message, got %d", len(msgs))
	}
}

func waitPmMessageFailed(t *testing.T, pm *services.PmService, tid, mid string) models.ChatMessage {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		m, err := pm.GetMessage(tid, mid)
		if err == nil && m.Status == "failed" {
			return m
		}
		if time.Now().After(deadline) {
			t.Fatalf("message %s never failed: %+v", mid, m)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPmLeaderNonAdminForbidden(t *testing.T) {
	hn := newHarness(t)
	cfg := config.GetConfig()
	users := make([]config.AuthUser, len(cfg.Auth.Users))
	copy(users, cfg.Auth.Users)
	for i := range users {
		users[i].IsAdmin = false
	}
	cfg.Auth.Users = users
	config.StoreConfig(cfg)

	pm := services.NewPmService(hn.db, hn.h.Agents)
	hn.h.Pm = pm
	hn.h.PMMCP = pmmcp.NewHost(pm, services.NewPmProgress(pm, hn.h.Runs, hn.h.Arts), nil, hn.h.Runs, hn.h.Arts, nil)

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "NoAdminProj"})
	if w.Code != 200 {
		t.Fatalf("create: %d", w.Code)
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid := proj["id"].(string)

	// Non-admin may enable/disable PM Leader binding.
	w = hn.do(http.MethodPut, "/api/projects/"+pid+"/pm-leader", map[string]any{
		"enabled": false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("non-admin pm-leader update want 200 got %d %s", w.Code, w.Body.String())
	}

	// Memory human writes remain admin-only.
	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/memories", map[string]any{
		"title": "x", "content": "y",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin memory want 403 got %d", w.Code)
	}
}

func TestListPmMemoriesScopedByRole(t *testing.T) {
	hn := newHarness(t)
	enableAdmin(t)

	pm := services.NewPmService(hn.db, nil)
	hn.h.Pm = pm
	hn.h.PMMCP = pmmcp.NewHost(pm, services.NewPmProgress(pm, hn.h.Runs, hn.h.Arts), nil, hn.h.Runs, hn.h.Arts, nil)

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "MemScopeProj"})
	if w.Code != 200 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid := proj["id"].(string)

	if _, err := pm.UpsertMemory(pid, "agent-a", "A", "secret-a", "agent", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpsertMemory(pid, "agent-b", "B", "secret-b", "agent", "b"); err != nil {
		t.Fatal(err)
	}
	en := true
	agentA := "agent-a"
	if _, err := pm.UpdateBinding(pid, &en, &agentA, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/memories", nil)
	if w.Code != 200 {
		t.Fatalf("admin list: %d %s", w.Code, w.Body.String())
	}
	var all struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &all)
	if len(all.Items) != 2 {
		t.Fatalf("admin want 2 items got %d", len(all.Items))
	}

	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/memories?agent=agent-b", nil)
	if w.Code != 200 {
		t.Fatalf("admin filter: %d", w.Code)
	}
	var filtered struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &filtered)
	if len(filtered.Items) != 1 || filtered.Items[0]["agentName"] != "agent-b" {
		t.Fatalf("admin ?agent=agent-b got %+v", filtered.Items)
	}

	cfg := config.GetConfig()
	users := make([]config.AuthUser, len(cfg.Auth.Users))
	copy(users, cfg.Auth.Users)
	for i := range users {
		users[i].IsAdmin = false
	}
	cfg.Auth.Users = users
	config.StoreConfig(cfg)

	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/memories?agent=agent-b", nil)
	if w.Code != 200 {
		t.Fatalf("non-admin list: %d %s", w.Code, w.Body.String())
	}
	var scoped struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &scoped)
	if len(scoped.Items) != 1 || scoped.Items[0]["agentName"] != "agent-a" {
		t.Fatalf("non-admin must see only PM agent-a, got %+v", scoped.Items)
	}

	empty := ""
	dis := false
	if _, err := pm.UpdateBinding(pid, &dis, &empty, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/memories", nil)
	if w.Code != 200 {
		t.Fatalf("unbound list: %d", w.Code)
	}
	var none struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &none)
	if len(none.Items) != 0 {
		t.Fatalf("unbound non-admin want empty got %+v", none.Items)
	}
}

func TestUpdatePmLeaderEnabledMcps(t *testing.T) {
	hn := newHarness(t)
	enableAdmin(t)

	pm := services.NewPmService(hn.db, nil)
	hn.h.Pm = pm

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "McpBindProj"})
	if w.Code != 200 {
		t.Fatalf("create: %d", w.Code)
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid := proj["id"].(string)

	w = hn.do(http.MethodPut, "/api/projects/"+pid+"/pm-leader", map[string]any{
		"enabled": true, "agentConfigRef": "agent-a",
		"enabledMcps": []string{"pm-progress", "memory-store", "pm-workflow-read"},
	})
	if w.Code != 200 {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var binding struct {
		EnabledMcps []string `json:"enabledMcps"`
		Enabled     bool     `json:"enabled"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &binding)
	if !binding.Enabled {
		t.Fatal("want enabled")
	}
	if len(binding.EnabledMcps) != 2 {
		t.Fatalf("want only pm-* mcps, got %v", binding.EnabledMcps)
	}
	for _, id := range binding.EnabledMcps {
		if id != "pm-progress" && id != "pm-workflow-read" {
			t.Fatalf("unexpected mcp %q", id)
		}
	}

	w = hn.do(http.MethodPut, "/api/projects/"+pid+"/pm-leader", map[string]any{
		"enabledMcps": []string{"pm-workflow-read"},
	})
	if w.Code != 200 {
		t.Fatalf("patch mcps: %d", w.Code)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &binding)
	if len(binding.EnabledMcps) != 1 || binding.EnabledMcps[0] != "pm-workflow-read" {
		t.Fatalf("got %v", binding.EnabledMcps)
	}

	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm-leader", nil)
	if w.Code != 200 {
		t.Fatalf("get: %d", w.Code)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &binding)
	if len(binding.EnabledMcps) != 1 || binding.EnabledMcps[0] != "pm-workflow-read" {
		t.Fatalf("persisted %v", binding.EnabledMcps)
	}
}

func TestProjectCronJobsListAndPatch(t *testing.T) {
	hn := newHarness(t)
	enableAdmin(t)

	pm := services.NewPmService(hn.db, hn.h.Agents)
	hn.h.Pm = pm

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "CronProjA"})
	if w.Code != 200 {
		t.Fatalf("create a: %d %s", w.Code, w.Body.String())
	}
	var projA map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &projA)
	pidA := projA["id"].(string)

	w = hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "CronProjB"})
	if w.Code != 200 {
		t.Fatalf("create b: %d", w.Code)
	}
	var projB map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &projB)
	pidB := projB["id"].(string)

	now := time.Now().UTC()
	jobs := []models.AgentCronJob{
		{
			ID: "cron-a1", AgentName: "agent-a", ProjectID: pidA, ThreadID: "th-a1",
			Name: "每日汇报", Prompt: "汇报", ScheduleKind: "cron", ScheduleExpr: "0 9 * * *",
			Enabled: true, DeliverToChannel: false, CreatedAt: now, UpdatedAt: now.Add(time.Minute),
		},
		{
			ID: "cron-a2", AgentName: "agent-b", ProjectID: pidA, ThreadID: "th-a2",
			Name: "每周扫描", Prompt: "扫描", ScheduleKind: "every", ScheduleExpr: "7d",
			Enabled: true, DeliverToChannel: true, CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: "cron-b1", AgentName: "agent-a", ProjectID: pidB, ThreadID: "th-b1",
			Name: "其他项目", Prompt: "x", ScheduleKind: "at", ScheduleExpr: now.Format(time.RFC3339),
			Enabled: true, DeliverToChannel: false, CreatedAt: now, UpdatedAt: now,
		},
	}
	for i := range jobs {
		if err := hn.db.Create(&jobs[i]).Error; err != nil {
			t.Fatalf("seed job: %v", err)
		}
	}

	w = hn.do(http.MethodGet, "/api/projects/"+pidA+"/cron-jobs", nil)
	if w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var list struct {
		Items []models.AgentCronJob `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Items) != 2 {
		t.Fatalf("want 2 jobs for project A, got %d", len(list.Items))
	}
	if list.Items[0].ID != "cron-a1" {
		t.Fatalf("want updated_at desc first cron-a1, got %s", list.Items[0].ID)
	}
	agents := map[string]bool{}
	for _, j := range list.Items {
		agents[j.AgentName] = true
		if j.ProjectID != pidA {
			t.Fatalf("cross-project leak: %+v", j)
		}
	}
	if !agents["agent-a"] || !agents["agent-b"] {
		t.Fatalf("want both agents, got %v", agents)
	}

	w = hn.do(http.MethodGet, "/api/projects/"+pidB+"/cron-jobs", nil)
	if w.Code != 200 {
		t.Fatalf("list b: %d", w.Code)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].ID != "cron-b1" {
		t.Fatalf("project B isolation: %+v", list.Items)
	}

	w = hn.do(http.MethodPatch, "/api/projects/"+pidA+"/cron-jobs/cron-a1", map[string]any{
		"deliverToChannel": true,
	})
	if w.Code != 200 {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}
	var patched models.AgentCronJob
	_ = json.Unmarshal(w.Body.Bytes(), &patched)
	if !patched.DeliverToChannel {
		t.Fatal("want deliverToChannel true")
	}

	var stored models.AgentCronJob
	if err := hn.db.Where("id = ?", "cron-a1").First(&stored).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !stored.DeliverToChannel {
		t.Fatal("deliverToChannel not persisted")
	}

	w = hn.do(http.MethodPatch, "/api/projects/"+pidA+"/cron-jobs/cron-b1", map[string]any{
		"deliverToChannel": true,
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-project patch want 404 got %d %s", w.Code, w.Body.String())
	}
}

func TestProjectCronJobsPatchAllowedForNonAdmin(t *testing.T) {
	hn := newHarness(t)
	enableAdmin(t)

	pm := services.NewPmService(hn.db, hn.h.Agents)
	hn.h.Pm = pm

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "CronNoAdmin"})
	if w.Code != 200 {
		t.Fatalf("create: %d", w.Code)
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid := proj["id"].(string)

	now := time.Now().UTC()
	job := models.AgentCronJob{
		ID: "cron-na1", AgentName: "agent-a", ProjectID: pid, ThreadID: "th-na1",
		Name: "任务", Prompt: "p", ScheduleKind: "every", ScheduleExpr: "1h",
		Enabled: true, DeliverToChannel: false, CreatedAt: now, UpdatedAt: now,
	}
	if err := hn.db.Create(&job).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg := config.GetConfig()
	users := make([]config.AuthUser, len(cfg.Auth.Users))
	copy(users, cfg.Auth.Users)
	for i := range users {
		users[i].IsAdmin = false
	}
	cfg.Auth.Users = users
	config.StoreConfig(cfg)

	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/cron-jobs", nil)
	if w.Code != 200 {
		t.Fatalf("non-admin list want 200 got %d", w.Code)
	}

	w = hn.do(http.MethodPatch, "/api/projects/"+pid+"/cron-jobs/cron-na1", map[string]any{
		"deliverToChannel": true,
	})
	if w.Code != 200 {
		t.Fatalf("non-admin patch want 200 got %d %s", w.Code, w.Body.String())
	}
	var patched models.AgentCronJob
	_ = json.Unmarshal(w.Body.Bytes(), &patched)
	if !patched.DeliverToChannel {
		t.Fatal("want deliverToChannel true for non-admin patch")
	}

	var stored models.AgentCronJob
	if err := hn.db.Where("id = ?", "cron-na1").First(&stored).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !stored.DeliverToChannel {
		t.Fatal("deliverToChannel not persisted for non-admin patch")
	}
}

func TestProjectCronJobsDeleteCleansRunsAndThread(t *testing.T) {
	hn := newHarness(t)
	enableAdmin(t)

	pm := services.NewPmService(hn.db, hn.h.Agents)
	hn.h.Pm = pm

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "CronDelA"})
	if w.Code != 200 {
		t.Fatalf("create a: %d %s", w.Code, w.Body.String())
	}
	var projA map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &projA)
	pidA := projA["id"].(string)

	w = hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "CronDelB"})
	if w.Code != 200 {
		t.Fatalf("create b: %d", w.Code)
	}
	var projB map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &projB)
	pidB := projB["id"].(string)

	now := time.Now().UTC()
	thread := models.ChatThread{
		ID: "th-del-1", ProjectID: pidA, UserID: "system", AgentName: "agent-a",
		Kind: "cron", Title: "cron thread", CreatedAt: now, UpdatedAt: now,
	}
	if err := hn.db.Create(&thread).Error; err != nil {
		t.Fatalf("seed thread: %v", err)
	}
	msg := models.ChatMessage{
		ID: "msg-del-1", ThreadID: thread.ID, Role: "user", Content: "hello", CreatedAt: now,
	}
	if err := hn.db.Create(&msg).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}
	draft := models.ChatTurnDraft{
		ID: "draft-del-1", ThreadID: thread.ID, Status: "done", CreatedAt: now, UpdatedAt: now,
	}
	if err := hn.db.Create(&draft).Error; err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	job := models.AgentCronJob{
		ID: "cron-del-1", AgentName: "agent-a", ProjectID: pidA, ThreadID: thread.ID,
		Name: "待删任务", Prompt: "p", ScheduleKind: "every", ScheduleExpr: "1h",
		Enabled: true, DeliverToChannel: false, CreatedAt: now, UpdatedAt: now,
	}
	if err := hn.db.Create(&job).Error; err != nil {
		t.Fatalf("seed job: %v", err)
	}
	run := models.AgentCronRun{
		ID: "run-del-1", JobID: job.ID, Status: "ok", StartedAt: now,
	}
	if err := hn.db.Create(&run).Error; err != nil {
		t.Fatalf("seed run: %v", err)
	}
	other := models.AgentCronJob{
		ID: "cron-del-other", AgentName: "agent-a", ProjectID: pidB, ThreadID: "th-other",
		Name: "其他项目", Prompt: "x", ScheduleKind: "every", ScheduleExpr: "1h",
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := hn.db.Create(&other).Error; err != nil {
		t.Fatalf("seed other: %v", err)
	}

	w = hn.do(http.MethodDelete, "/api/projects/"+pidA+"/cron-jobs/cron-del-other", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-project delete want 404 got %d %s", w.Code, w.Body.String())
	}
	var stillOther models.AgentCronJob
	if err := hn.db.Where("id = ?", "cron-del-other").First(&stillOther).Error; err != nil {
		t.Fatalf("cross-project delete must not remove other job: %v", err)
	}

	w = hn.do(http.MethodDelete, "/api/projects/"+pidA+"/cron-jobs/cron-del-1", nil)
	if w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	var delResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &delResp)
	if delResp["status"] != "deleted" {
		t.Fatalf("want status deleted, got %+v", delResp)
	}

	if err := hn.db.Where("id = ?", "cron-del-1").First(&models.AgentCronJob{}).Error; err == nil {
		t.Fatal("job should be deleted")
	}
	var runCount int64
	if err := hn.db.Model(&models.AgentCronRun{}).Where("job_id = ?", "cron-del-1").Count(&runCount).Error; err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if runCount != 0 {
		t.Fatalf("want runs cleaned, got %d", runCount)
	}
	if err := hn.db.Where("id = ?", thread.ID).First(&models.ChatThread{}).Error; err == nil {
		t.Fatal("thread should be deleted")
	}
	var msgCount int64
	if err := hn.db.Model(&models.ChatMessage{}).Where("thread_id = ?", thread.ID).Count(&msgCount).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if msgCount != 0 {
		t.Fatalf("want messages cleaned, got %d", msgCount)
	}
	var draftCount int64
	if err := hn.db.Model(&models.ChatTurnDraft{}).Where("thread_id = ?", thread.ID).Count(&draftCount).Error; err != nil {
		t.Fatalf("count drafts: %v", err)
	}
	if draftCount != 0 {
		t.Fatalf("want drafts cleaned, got %d", draftCount)
	}

	w = hn.do(http.MethodGet, "/api/projects/"+pidA+"/cron-jobs", nil)
	if w.Code != 200 {
		t.Fatalf("list after delete: %d", w.Code)
	}
	var list struct {
		Items []models.AgentCronJob `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Items) != 0 {
		t.Fatalf("want empty list after delete, got %+v", list.Items)
	}
}

func TestProjectCronJobsDeleteAllowedForNonAdmin(t *testing.T) {
	hn := newHarness(t)
	enableAdmin(t)

	pm := services.NewPmService(hn.db, hn.h.Agents)
	hn.h.Pm = pm

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "CronDelNoAdmin"})
	if w.Code != 200 {
		t.Fatalf("create: %d", w.Code)
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid := proj["id"].(string)

	now := time.Now().UTC()
	job := models.AgentCronJob{
		ID: "cron-del-na1", AgentName: "agent-a", ProjectID: pid, ThreadID: "",
		Name: "任务", Prompt: "p", ScheduleKind: "every", ScheduleExpr: "1h",
		Enabled: true, DeliverToChannel: false, CreatedAt: now, UpdatedAt: now,
	}
	if err := hn.db.Create(&job).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg := config.GetConfig()
	users := make([]config.AuthUser, len(cfg.Auth.Users))
	copy(users, cfg.Auth.Users)
	for i := range users {
		users[i].IsAdmin = false
	}
	cfg.Auth.Users = users
	config.StoreConfig(cfg)

	w = hn.do(http.MethodDelete, "/api/projects/"+pid+"/cron-jobs/cron-del-na1", nil)
	if w.Code != 200 {
		t.Fatalf("non-admin delete want 200 got %d %s", w.Code, w.Body.String())
	}
	if err := hn.db.Where("id = ?", "cron-del-na1").First(&models.AgentCronJob{}).Error; err == nil {
		t.Fatal("job should be deleted for non-admin")
	}
}

func setupPmEnabledHarness(t *testing.T) (*harness, string, string) {
	t.Helper()
	hn := newHarness(t)
	enableAdmin(t)
	hn.cookie = hn.login(t)
	pm := services.NewPmService(hn.db, hn.h.Agents)
	hn.h.Pm = pm
	hn.h.PmProgress = services.NewPmProgress(pm, hn.h.Runs, hn.h.Arts)
	hn.h.PMMCP = pmmcp.NewHost(pm, hn.h.PmProgress, nil, hn.h.Runs, hn.h.Arts, nil)
	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "PmHTTP"})
	if w.Code != 200 {
		t.Fatalf("create project: %d %s", w.Code, w.Body.String())
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid := proj["id"].(string)
	// A PM Leader must have this project as its home project (Agent↔project binding).
	if err := hn.h.Agents.Save(services.Agent{Name: "pm-agent", ProjectID: pid, Env: map[string]string{"GRASP_CURSOR_API_KEY": "test-key"}}); err != nil {
		t.Fatal(err)
	}
	w = hn.do(http.MethodPut, "/api/projects/"+pid+"/pm-leader", map[string]any{
		"enabled": true, "agentConfigRef": "pm-agent",
	})
	if w.Code != 200 {
		t.Fatalf("enable pm: %d %s", w.Code, w.Body.String())
	}
	return hn, pid, ""
}

func TestPmMemoryUpdateDeleteAndWritePmErr(t *testing.T) {
	hn, pid, _ := setupPmEnabledHarness(t)

	w := hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/memories", map[string]any{
		"title": "背景", "content": "Go 项目",
	})
	if w.Code != 200 {
		t.Fatalf("upsert: %d %s", w.Code, w.Body.String())
	}
	var mem map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &mem)
	mid := mem["id"].(string)

	w = hn.do(http.MethodPut, "/api/projects/"+pid+"/pm/memories/"+mid, map[string]any{
		"title": "更新", "content": "新内容",
	})
	if w.Code != 200 {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var updated map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if updated["content"] != "新内容" {
		t.Fatalf("updated=%v", updated)
	}

	w = hn.do(http.MethodPut, "/api/projects/"+pid+"/pm/memories/missing-id", map[string]any{
		"title": "x", "content": "y",
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("update missing want 404 got %d", w.Code)
	}

	w = hn.do(http.MethodDelete, "/api/projects/"+pid+"/pm/memories/"+mid, nil)
	if w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodDelete, "/api/projects/"+pid+"/pm/memories/"+mid, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("delete twice want 404 got %d", w.Code)
	}
}

func TestPmThreadCRUDAndMessages(t *testing.T) {
	hn, pid, _ := setupPmEnabledHarness(t)

	w := hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads", map[string]any{"title": "进度"})
	if w.Code != 200 {
		t.Fatalf("create thread: %d %s", w.Code, w.Body.String())
	}
	var thr map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &thr)
	tid := thr["id"].(string)

	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/threads", nil)
	if w.Code != 200 {
		t.Fatalf("list threads: %d", w.Code)
	}
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/threads/"+tid, nil)
	if w.Code != 200 {
		t.Fatalf("get thread: %d %s", w.Code, w.Body.String())
	}

	for i := 0; i < 3; i++ {
		w = hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages", map[string]any{
			"role": "user", "content": "msg",
		})
		if w.Code != 200 {
			t.Fatalf("append %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages", nil)
	if w.Code != 200 {
		t.Fatalf("list messages: %d", w.Code)
	}
	var msgs struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &msgs)
	if len(msgs.Items) != 3 {
		t.Fatalf("messages=%d", len(msgs.Items))
	}

	w = hn.do(http.MethodDelete, "/api/projects/"+pid+"/pm/threads/"+tid, nil)
	if w.Code != 200 {
		t.Fatalf("delete thread: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/threads/"+tid, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get deleted want 404 got %d", w.Code)
	}
}

func TestPatchPmMessageFailureMetadata(t *testing.T) {
	hn, pid, _ := setupPmEnabledHarness(t)
	w := hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads", map[string]any{"title": "patch"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var thr map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &thr)
	tid := thr["id"].(string)
	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages", map[string]any{
		"role": "user", "content": "fail me",
	})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var msg map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &msg)
	mid := msg["id"].(string)

	w = hn.do(http.MethodPatch, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages/"+mid, map[string]any{
		"status": "failed", "failKind": "connection",
	})
	if w.Code != 200 {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPatch, "/api/projects/"+pid+"/pm/threads/"+tid+"/messages/missing", map[string]any{
		"status": "failed",
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("patch missing want 404 got %d", w.Code)
	}
}

func TestPmUpsertMemoryBadRequest(t *testing.T) {
	hn, pid, _ := setupPmEnabledHarness(t)
	w := hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/memories", map[string]any{
		"title": "", "content": "",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty memory want 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestPmThreadDisabledReturnsConflict(t *testing.T) {
	hn := newHarness(t)
	enableAdmin(t)
	pm := services.NewPmService(hn.db, nil)
	hn.h.Pm = pm
	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "DisabledPM"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid := proj["id"].(string)
	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads", map[string]any{})
	if w.Code != http.StatusConflict {
		t.Fatalf("disabled thread create want 409 got %d", w.Code)
	}
}

func TestPmChannelThreadWriteDeleteForbidden(t *testing.T) {
	hn, pid, _ := setupPmEnabledHarness(t)
	hn.h.PmTurns = services.NewPmTurnRunner(hn.h.Pm, hn.h.Sbx)

	channel, err := hn.h.Pm.CreateThread(pid, "qq:guild:ch1", "频道会话", "pm-agent", "user")
	if err != nil {
		t.Fatalf("create channel thread: %v", err)
	}

	w := hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads", map[string]any{"title": "Web会话"})
	if w.Code != 200 {
		t.Fatalf("create web thread: %d %s", w.Code, w.Body.String())
	}
	var web map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &web)
	webTID := web["id"].(string)

	assertChannelReadOnly := func(t *testing.T, label string, code int, body string) {
		t.Helper()
		if code != http.StatusForbidden {
			t.Fatalf("%s want 403 got %d %s", label, code, body)
		}
		if !strings.Contains(body, "渠道会话") || (!strings.Contains(body, "只读") && !strings.Contains(body, "不可在 Web 改写")) {
			t.Fatalf("%s want channel read-only error, got %s", label, body)
		}
	}

	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads/"+channel.ID+"/messages", map[string]any{
		"role": "user", "content": "不应发送",
	})
	assertChannelReadOnly(t, "append channel", w.Code, w.Body.String())

	w = hn.do(http.MethodDelete, "/api/projects/"+pid+"/pm/threads/"+channel.ID, nil)
	assertChannelReadOnly(t, "delete channel", w.Code, w.Body.String())

	w = hn.do(http.MethodGet, "/api/projects/"+pid+"/pm/threads/"+channel.ID+"/chat", nil)
	assertChannelReadOnly(t, "ws chat channel", w.Code, w.Body.String())

	w = hn.do(http.MethodPost, "/api/projects/"+pid+"/pm/threads/"+webTID+"/messages", map[string]any{
		"role": "user", "content": "web ok",
	})
	if w.Code != 200 {
		t.Fatalf("append web: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodDelete, "/api/projects/"+pid+"/pm/threads/"+webTID, nil)
	if w.Code != 200 {
		t.Fatalf("delete web: %d %s", w.Code, w.Body.String())
	}
}
