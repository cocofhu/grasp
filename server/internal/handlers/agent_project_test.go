package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/pmmcp"
	"github.com/cocofhu/grasp/internal/services"
)

func attachPm(t *testing.T, hn *harness) *services.PmService {
	t.Helper()
	pm := services.NewPmService(hn.db, hn.h.Agents)
	hn.h.Pm = pm
	hn.h.PmProgress = services.NewPmProgress(pm, hn.h.Runs, hn.h.Arts)
	hn.h.PMMCP = pmmcp.NewHost(pm, hn.h.PmProgress, nil, hn.h.Runs, hn.h.Arts, nil)
	return pm
}

func grantAdmin(t *testing.T) {
	t.Helper()
	cfg := config.GetConfig()
	users := append([]config.AuthUser(nil), cfg.Auth.Users...)
	for i := range users {
		if users[i].Username == "admin" {
			users[i].IsAdmin = true
		}
	}
	cfg.Auth.Users = users
	config.StoreConfig(cfg)
	t.Cleanup(func() {
		cfg := config.GetConfig()
		users := append([]config.AuthUser(nil), cfg.Auth.Users...)
		for i := range users {
			if users[i].Username == "admin" {
				users[i].IsAdmin = false
			}
		}
		cfg.Auth.Users = users
		config.StoreConfig(cfg)
	})
}

func TestAgentProjectIDRequired(t *testing.T) {
	hn := newHarness(t)
	w := hn.do(http.MethodPost, "/api/agents", map[string]any{"acpBackend": "cursor", "name": "no-home"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create without projectId want 400 got %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPost, "/api/agents", map[string]any{"acpBackend": "cursor", "name": "no-home", "projectId": "proj_dead"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create with missing project want 400 got %d %s", w.Code, w.Body.String())
	}
	if hn.h.Agents.Exists("no-home") {
		t.Fatal("rejected create must not persist")
	}

	w = hn.do(http.MethodPost, "/api/agents", map[string]any{"acpBackend": "cursor", "name": "home", "projectId": models.DefaultProjectID})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPut, "/api/agents/home", map[string]any{"acpBackend": "cursor", "name": "home"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("save without projectId want 400 got %d %s", w.Code, w.Body.String())
	}
	ag, ok := hn.h.Agents.Get("home")
	if !ok || ag.ProjectID != models.DefaultProjectID {
		t.Fatalf("binding must survive rejected save, got %+v", ag)
	}
}

func TestAgentSaveRejectsSecretEnvKeys(t *testing.T) {
	hn := newHarness(t)
	for _, key := range envauth.SecretEnvKeys() {
		w := hn.do(http.MethodPost, "/api/agents", map[string]any{"acpBackend": "cursor",
			"name": "ssh-env", "projectId": models.DefaultProjectID,
			"env": map[string]string{key: "x"},
		})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("create with %s want 400 got %d %s", key, w.Code, w.Body.String())
		}
	}
	w := hn.do(http.MethodPost, "/api/agents", map[string]any{"acpBackend": "cursor", "name": "ssh-env", "projectId": models.DefaultProjectID})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPut, "/api/agents/ssh-env", map[string]any{"acpBackend": "cursor",
		"name": "ssh-env", "projectId": models.DefaultProjectID,
		"env": map[string]string{"GIT_SSH_PRIVATE_KEY": "x"},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("save with secret env want 400 got %d %s", w.Code, w.Body.String())
	}
}

func TestSaveAgentPurgesOnProjectSwitch(t *testing.T) {
	hn := newHarness(t)
	pm := attachPm(t, hn)

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "OldHome"})
	if w.Code != 200 {
		t.Fatalf("create project A: %d %s", w.Code, w.Body.String())
	}
	var projA map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &projA)
	pidA := projA["id"].(string)

	w = hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "NewHome"})
	if w.Code != 200 {
		t.Fatalf("create project B: %d %s", w.Code, w.Body.String())
	}
	var projB map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &projB)
	pidB := projB["id"].(string)

	w = hn.do(http.MethodPost, "/api/agents", map[string]any{"acpBackend": "cursor",
		"name": "switcher", "projectId": pidA,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create agent: %d %s", w.Code, w.Body.String())
	}
	if _, err := pm.UpsertMemory(pidA, "switcher", "记", "old", "agent", "u"); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.CreateThread(pidA, "admin", "t", "switcher", models.ChatThreadKindUser); err != nil {
		t.Fatal(err)
	}

	w = hn.do(http.MethodPut, "/api/agents/switcher", map[string]any{"acpBackend": "cursor",
		"name": "switcher", "projectId": pidB,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("switch project: %d %s", w.Code, w.Body.String())
	}
	mem, _ := pm.ListMemories(pidA, "switcher")
	if len(mem) != 0 {
		t.Fatalf("old memories should be purged: %v", mem)
	}
	threads, _ := pm.ListThreadsForAgent(pidA, "switcher")
	if len(threads) != 0 {
		t.Fatalf("old threads should be purged: %v", threads)
	}
}

func TestDeleteAgentPurgesScopedData(t *testing.T) {
	hn := newHarness(t)
	pm := attachPm(t, hn)

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "DelHome"})
	if w.Code != 200 {
		t.Fatalf("project: %d %s", w.Code, w.Body.String())
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid := proj["id"].(string)

	w = hn.do(http.MethodPost, "/api/agents", map[string]any{"acpBackend": "cursor",
		"name": "to-delete", "projectId": pid,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	if _, err := pm.UpsertMemory(pid, "to-delete", "M", "c", "admin", "u"); err != nil {
		t.Fatal(err)
	}
	w = hn.do(http.MethodDelete, "/api/agents/to-delete", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	mem, _ := pm.ListMemories(pid, "to-delete")
	if len(mem) != 0 {
		t.Fatalf("memories should be purged: %v", mem)
	}
}

func TestAgentDataAPIUsesHomeProject(t *testing.T) {
	hn := newHarness(t)
	attachPm(t, hn)
	grantAdmin(t)

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "DataHome"})
	if w.Code != 200 {
		t.Fatalf("project: %d %s", w.Code, w.Body.String())
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid := proj["id"].(string)

	w = hn.do(http.MethodPost, "/api/agents", map[string]any{"acpBackend": "cursor", "name": "data-agent", "projectId": pid})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}

	w = hn.do(http.MethodPost, "/api/agents/data-agent/memories", map[string]any{
		"title": "T", "content": "C",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("upsert memory: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodGet, "/api/agents/data-agent/memories", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list memories: %d %s", w.Code, w.Body.String())
	}
	var listed struct {
		Items []models.ProjectMemoryItem `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listed)
	if len(listed.Items) != 1 || listed.Items[0].AgentName != "data-agent" {
		t.Fatalf("listed=%+v", listed.Items)
	}

	w = hn.do(http.MethodGet, "/api/agents/data-agent/threads", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list threads: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodGet, "/api/agents/data-agent/cron-jobs", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list jobs: %d %s", w.Code, w.Body.String())
	}
}

// TestAgentCronJobsListAllowedForNonAdmin covers Agent-data ACL alignment:
// non-admin GET cron-jobs / memories CRUD / threads / PATCH|DELETE cron-jobs → 200;
// admin write path still works.
func TestAgentCronJobsListAllowedForNonAdmin(t *testing.T) {
	hn := newHarness(t)
	attachPm(t, hn)
	grantAdmin(t)

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "CronACLHome"})
	if w.Code != 200 {
		t.Fatalf("project: %d %s", w.Code, w.Body.String())
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid := proj["id"].(string)

	w = hn.do(http.MethodPost, "/api/agents", map[string]any{"acpBackend": "cursor", "name": "cron-acl-agent", "projectId": pid})
	if w.Code != http.StatusCreated {
		t.Fatalf("create agent: %d %s", w.Code, w.Body.String())
	}

	now := time.Now().UTC()
	job := models.AgentCronJob{
		ID: "cron-acl-1", AgentName: "cron-acl-agent", ProjectID: pid, ThreadID: "th-acl-1",
		Name: "任务", Prompt: "p", ScheduleKind: "every", ScheduleExpr: "1h",
		Enabled: true, DeliverToChannel: false, CreatedAt: now, UpdatedAt: now,
	}
	if err := hn.db.Create(&job).Error; err != nil {
		t.Fatalf("seed job: %v", err)
	}
	thread := models.ChatThread{
		ID: "th-user-acl-1", ProjectID: pid, UserID: "other-user",
		AgentName: "cron-acl-agent", Kind: models.ChatThreadKindUser,
		Title: "他人会话", CreatedAt: now, UpdatedAt: now,
	}
	if err := hn.db.Create(&thread).Error; err != nil {
		t.Fatalf("seed thread: %v", err)
	}
	msg := models.ChatMessage{
		ID: "msg-acl-1", ThreadID: thread.ID, Role: "user", Content: "hello",
		CreatedAt: now,
	}
	if err := hn.db.Create(&msg).Error; err != nil {
		t.Fatalf("seed message: %v", err)
	}

	// Admin write path must not regress before demoting.
	w = hn.do(http.MethodPatch, "/api/agents/cron-acl-agent/cron-jobs/cron-acl-1", map[string]any{
		"enabled": false,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("admin patch want 200 got %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPatch, "/api/agents/cron-acl-agent/cron-jobs/cron-acl-1", map[string]any{
		"enabled": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("admin re-enable want 200 got %d %s", w.Code, w.Body.String())
	}

	cfg := config.GetConfig()
	users := make([]config.AuthUser, len(cfg.Auth.Users))
	copy(users, cfg.Auth.Users)
	for i := range users {
		users[i].IsAdmin = false
	}
	cfg.Auth.Users = users
	config.StoreConfig(cfg)

	w = hn.do(http.MethodGet, "/api/agents/cron-acl-agent/cron-jobs", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("non-admin list cron-jobs want 200 got %d %s", w.Code, w.Body.String())
	}
	var listed struct {
		Items []models.AgentCronJob `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listed)
	if len(listed.Items) != 1 || listed.Items[0].ID != "cron-acl-1" {
		t.Fatalf("listed=%+v", listed.Items)
	}

	w = hn.do(http.MethodPatch, "/api/agents/cron-acl-agent/cron-jobs/cron-acl-1", map[string]any{
		"deliverToChannel": true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("non-admin patch want 200 got %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodGet, "/api/agents/cron-acl-agent/memories", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("non-admin memories want 200 got %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPost, "/api/agents/cron-acl-agent/memories", map[string]any{
		"title": "非管理员记忆", "content": "ok",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("non-admin upsert memory want 200 got %d %s", w.Code, w.Body.String())
	}
	var mem models.ProjectMemoryItem
	if err := json.Unmarshal(w.Body.Bytes(), &mem); err != nil {
		t.Fatalf("decode memory: %v", err)
	}
	if mem.Source != "user" || mem.UpdatedBy == "" {
		t.Fatalf("memory source/updatedBy: %+v", mem)
	}
	w = hn.do(http.MethodPut, "/api/agents/cron-acl-agent/memories/"+mem.ID, map[string]any{
		"title": "非管理员记忆", "content": "updated",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("non-admin update memory want 200 got %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodDelete, "/api/agents/cron-acl-agent/memories/"+mem.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("non-admin delete memory want 200 got %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodPost, "/api/agents/cron-acl-agent/memories", map[string]any{
		"title": "to-clear", "content": "x",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("seed clear: %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodDelete, "/api/agents/cron-acl-agent/memories", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("non-admin clear memories want 200 got %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodGet, "/api/agents/cron-acl-agent/threads", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("non-admin threads want 200 got %d %s", w.Code, w.Body.String())
	}
	var threadsListed struct {
		Items []models.ChatThread `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &threadsListed)
	if len(threadsListed.Items) == 0 {
		t.Fatalf("expected seeded thread in list, got %+v", threadsListed.Items)
	}
	w = hn.do(http.MethodGet, "/api/agents/cron-acl-agent/threads/"+thread.ID+"/messages", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("non-admin thread messages want 200 got %d %s", w.Code, w.Body.String())
	}
	var msgsListed struct {
		Items []models.ChatMessage `json:"items"`
		Total int                  `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &msgsListed)
	if msgsListed.Total != 1 || len(msgsListed.Items) != 1 || msgsListed.Items[0].Content != "hello" {
		t.Fatalf("messages=%+v total=%d", msgsListed.Items, msgsListed.Total)
	}
	w = hn.do(http.MethodDelete, "/api/agents/cron-acl-agent/threads/"+thread.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("non-admin delete thread want 200 got %d %s", w.Code, w.Body.String())
	}
	w = hn.do(http.MethodDelete, "/api/agents/cron-acl-agent/cron-jobs/cron-acl-1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("non-admin delete cron want 200 got %d %s", w.Code, w.Body.String())
	}
}

func TestPmLeaderBindRejectsWrongHomeProject(t *testing.T) {
	hn := newHarness(t)
	attachPm(t, hn)

	w := hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "PM1"})
	if w.Code != 200 {
		t.Fatalf("project: %d %s", w.Code, w.Body.String())
	}
	var proj map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &proj)
	pid := proj["id"].(string)

	w = hn.do(http.MethodPost, "/api/projects", map[string]any{"name": "Other"})
	if w.Code != 200 {
		t.Fatalf("other: %d %s", w.Code, w.Body.String())
	}
	var other map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &other)

	if err := hn.h.Agents.Save(services.Agent{
		AcpBackend: services.AcpBackendCursor,
		Name:       "elsewhere", ProjectID: other["id"].(string),
	}); err != nil {
		t.Fatal(err)
	}
	w = hn.do(http.MethodPut, "/api/projects/"+pid+"/pm-leader", map[string]any{
		"enabled": true, "agentConfigRef": "elsewhere",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409 mismatch got %d %s", w.Code, w.Body.String())
	}
}
