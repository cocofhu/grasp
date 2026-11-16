package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/modelrouting"
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

	w := hn.do(http.MethodPut, "/api/workspace/pm-leader", map[string]any{
		"enabled": true, "agentConfigRef": "no-such-agent",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("enable missing agent: %d %s", w.Code, w.Body.String())
	}

	w = hn.do(http.MethodPut, "/api/workspace/pm-leader", map[string]any{
		"enabled": false, "agentConfigRef": "",
	})
	if w.Code != 200 {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}

	w = hn.do(http.MethodGet, "/api/workspace/pm-leader", nil)
	if w.Code != 200 {
		t.Fatalf("get binding: %d", w.Code)
	}

	w = hn.do(http.MethodGet, "/api/workspace/pm/memories", nil)
	if w.Code != 200 {
		t.Fatalf("list memories: %d", w.Code)
	}

	w = hn.do(http.MethodPost, "/api/workspace/pm/threads", map[string]any{})
	if w.Code != http.StatusConflict {
		t.Fatalf("create thread while disabled want 409 got %d %s", w.Code, w.Body.String())
	}

	if err := hn.h.Agents.Save(services.Agent{AcpBackend: services.AcpBackendCursor, Name: "pm-demo"}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	w = hn.do(http.MethodPut, "/api/workspace/pm-leader", map[string]any{
		"enabled": true, "agentConfigRef": "pm-demo",
	})
	if w.Code != 200 {
		t.Fatalf("enable: %d %s", w.Code, w.Body.String())
	}

	w = hn.do(http.MethodPost, "/api/workspace/pm/threads", map[string]any{"title": "问进度"})
	if w.Code != 200 {
		t.Fatalf("create thread: %d %s", w.Code, w.Body.String())
	}
	var thr map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &thr)
	tid := thr["id"].(string)

	w = hn.do(http.MethodPost, "/api/workspace/pm/threads/"+tid+"/messages", map[string]any{
		"role": "user", "content": "整体进度如何？",
	})
	if w.Code != 200 {
		t.Fatalf("append message: %d %s", w.Code, w.Body.String())
	}

	w = hn.do(http.MethodGet, "/api/workspace/pm/threads/"+tid+"/messages", nil)
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

	w = hn.do(http.MethodPatch, "/api/workspace/pm/threads/"+tid+"/messages/"+mid, map[string]any{
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

	w = hn.do(http.MethodPatch, "/api/workspace/pm/threads/"+tid+"/messages/"+mid, map[string]any{
		"status": "ok",
	})
	if w.Code != 200 {
		t.Fatalf("patch clear: %d %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &patched)
	if patched["status"] != "ok" {
		t.Fatalf("cleared status=%v", patched["status"])
	}
}

func setupPmTurnThread(t *testing.T, hn *harness, name string) (pm *services.PmService, tid string) {
	t.Helper()
	enableAdmin(t)
	pm = services.NewPmService(hn.db, hn.h.Agents)
	hn.h.Pm = pm
	hn.h.PmProgress = services.NewPmProgress(pm, hn.h.Runs, hn.h.Arts)
	hn.h.PMMCP = pmmcp.NewHost(pm, hn.h.PmProgress, nil, hn.h.Runs, hn.h.Arts, nil)

	if err := hn.h.Agents.Save(services.Agent{AcpBackend: services.AcpBackendCursor, Name: "pm-" + name}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	w := hn.do(http.MethodPut, "/api/workspace/pm-leader", map[string]any{
		"enabled": true, "agentConfigRef": "pm-" + name,
	})
	if w.Code != 200 {
		t.Fatalf("enable: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPost, "/api/workspace/pm/threads", map[string]any{"title": "t"})
	if w.Code != 200 {
		t.Fatalf("thread: %d %s", w.Code, w.Body.String())
	}
	var thr map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &thr)
	return pm, thr["id"].(string)
}

func TestStartPmTurnValidatesAndMarksRejected(t *testing.T) {
	hn := newHarness(t)
	pm, tid := setupPmTurnThread(t, hn, "TurnReject")
	hn.h.PmTurns = services.NewPmTurnRunner(pm, nil)
	base := "/api/workspace/pm/threads/" + tid

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
	pm, tid := setupPmTurnThread(t, hn, "TurnQueue")
	hn.fg.FailCreate = true
	runner := services.NewPmTurnRunner(pm, nil)
	runner.SetChatterForTest(stubPmChat{})
	hn.h.PmTurns = runner
	base := "/api/workspace/pm/threads/" + tid

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

	// Non-admin may enable/disable PM Leader binding.
	w := hn.do(http.MethodPut, "/api/workspace/pm-leader", map[string]any{
		"enabled": false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("non-admin pm-leader update want 200 got %d %s", w.Code, w.Body.String())
	}
}

func TestListPmMemoriesScopedByRole(t *testing.T) {
	hn := newHarness(t)
	enableAdmin(t)

	pm := services.NewPmService(hn.db, nil)
	hn.h.Pm = pm
	hn.h.PMMCP = pmmcp.NewHost(pm, services.NewPmProgress(pm, hn.h.Runs, hn.h.Arts), nil, hn.h.Runs, hn.h.Arts, nil)

	if _, err := pm.UpsertMemory("agent-a", "A", "secret-a", "agent", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpsertMemory("agent-b", "B", "secret-b", "agent", "b"); err != nil {
		t.Fatal(err)
	}
	en := true
	agentA := "agent-a"
	if _, err := pm.UpdateBinding(&en, &agentA, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	w := hn.do(http.MethodGet, "/api/workspace/pm/memories", nil)
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

	w = hn.do(http.MethodGet, "/api/workspace/pm/memories?agent=agent-b", nil)
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

	w = hn.do(http.MethodGet, "/api/workspace/pm/memories?agent=agent-b", nil)
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
	if _, err := pm.UpdateBinding(&dis, &empty, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	w = hn.do(http.MethodGet, "/api/workspace/pm/memories", nil)
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

	w := hn.do(http.MethodPut, "/api/workspace/pm-leader", map[string]any{
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

	w = hn.do(http.MethodPut, "/api/workspace/pm-leader", map[string]any{
		"enabledMcps": []string{"pm-workflow-read"},
	})
	if w.Code != 200 {
		t.Fatalf("patch mcps: %d", w.Code)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &binding)
	if len(binding.EnabledMcps) != 1 || binding.EnabledMcps[0] != "pm-workflow-read" {
		t.Fatalf("got %v", binding.EnabledMcps)
	}

	w = hn.do(http.MethodGet, "/api/workspace/pm-leader", nil)
	if w.Code != 200 {
		t.Fatalf("get: %d", w.Code)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &binding)
	if len(binding.EnabledMcps) != 1 || binding.EnabledMcps[0] != "pm-workflow-read" {
		t.Fatalf("persisted %v", binding.EnabledMcps)
	}
}

func setupPmEnabledHarness(t *testing.T) *harness {
	t.Helper()
	hn := newHarness(t)
	enableAdmin(t)
	hn.cookie = hn.login(t)
	pm := services.NewPmService(hn.db, hn.h.Agents)
	hn.h.Pm = pm
	hn.h.PmProgress = services.NewPmProgress(pm, hn.h.Runs, hn.h.Arts)
	hn.h.PMMCP = pmmcp.NewHost(pm, hn.h.PmProgress, nil, hn.h.Runs, hn.h.Arts, nil)

	if err := hn.h.Agents.Save(services.Agent{AcpBackend: services.AcpBackendCursor, Name: "pm-agent", ModelBinding: &modelrouting.Binding{Kind: "configuration", ConfigurationID: "handler-model"}}); err != nil {
		t.Fatal(err)
	}
	w := hn.do(http.MethodPut, "/api/workspace/pm-leader", map[string]any{
		"enabled": true, "agentConfigRef": "pm-agent",
	})
	if w.Code != 200 {
		t.Fatalf("enable pm: %d %s", w.Code, w.Body.String())
	}
	return hn
}

func TestPmThreadCRUDAndMessages(t *testing.T) {
	hn := setupPmEnabledHarness(t)

	w := hn.do(http.MethodPost, "/api/workspace/pm/threads", map[string]any{"title": "进度"})
	if w.Code != 200 {
		t.Fatalf("create thread: %d %s", w.Code, w.Body.String())
	}
	var thr map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &thr)
	tid := thr["id"].(string)

	w = hn.do(http.MethodGet, "/api/workspace/pm/threads", nil)
	if w.Code != 200 {
		t.Fatalf("list threads: %d", w.Code)
	}
	w = hn.do(http.MethodGet, "/api/workspace/pm/threads/"+tid, nil)
	if w.Code != 200 {
		t.Fatalf("get thread: %d %s", w.Code, w.Body.String())
	}

	for i := 0; i < 3; i++ {
		w = hn.do(http.MethodPost, "/api/workspace/pm/threads/"+tid+"/messages", map[string]any{
			"role": "user", "content": "msg",
		})
		if w.Code != 200 {
			t.Fatalf("append %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	w = hn.do(http.MethodGet, "/api/workspace/pm/threads/"+tid+"/messages", nil)
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

	w = hn.do(http.MethodDelete, "/api/workspace/pm/threads/"+tid, nil)
	if w.Code != 200 {
		t.Fatalf("delete thread: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodGet, "/api/workspace/pm/threads/"+tid, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get deleted want 404 got %d", w.Code)
	}
}

func TestPatchPmMessageFailureMetadata(t *testing.T) {
	hn := setupPmEnabledHarness(t)
	w := hn.do(http.MethodPost, "/api/workspace/pm/threads", map[string]any{"title": "patch"})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var thr map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &thr)
	tid := thr["id"].(string)
	w = hn.do(http.MethodPost, "/api/workspace/pm/threads/"+tid+"/messages", map[string]any{
		"role": "user", "content": "fail me",
	})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var msg map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &msg)
	mid := msg["id"].(string)

	w = hn.do(http.MethodPatch, "/api/workspace/pm/threads/"+tid+"/messages/"+mid, map[string]any{
		"status": "failed", "failKind": "connection",
	})
	if w.Code != 200 {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPatch, "/api/workspace/pm/threads/"+tid+"/messages/missing", map[string]any{
		"status": "failed",
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("patch missing want 404 got %d", w.Code)
	}
}

func TestPmThreadDisabledReturnsConflict(t *testing.T) {
	hn := newHarness(t)
	enableAdmin(t)
	pm := services.NewPmService(hn.db, nil)
	hn.h.Pm = pm
	w := hn.do(http.MethodPost, "/api/workspace/pm/threads", map[string]any{})
	if w.Code != http.StatusConflict {
		t.Fatalf("disabled thread create want 409 got %d", w.Code)
	}
}
