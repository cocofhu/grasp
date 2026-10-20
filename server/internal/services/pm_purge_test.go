package services

import (
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestPurgeAgentEverywhere(t *testing.T) {
	db := setupPmDB(t)
	skills := NewAgentService(t.TempDir())
	pm := NewPmService(db, skills)
	ps := NewWorkspaceService(db)
	if _, err := ps.CreateFixture("Purge", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := skills.Save(Agent{AcpBackend: AcpBackendCursor, Name: "agent-x"}); err != nil {
		t.Fatal(err)
	}
	if err := skills.Save(Agent{AcpBackend: AcpBackendCursor, Name: "agent-y"}); err != nil {
		t.Fatal(err)
	}

	if _, err := pm.UpsertMemory("agent-x", "X记", "x-content", "agent", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpsertMemory("agent-y", "Y记", "y-content", "agent", "y"); err != nil {
		t.Fatal(err)
	}
	thX, err := pm.CreateThread("alice", "x-thread", "agent-x", models.ChatThreadKindUser)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.AppendMessage(thX.ID, "user", "hello", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	thY, err := pm.CreateThread("bob", "y-thread", "agent-y", models.ChatThreadKindUser)
	if err != nil {
		t.Fatal(err)
	}
	jobX := models.AgentCronJob{
		ID: "job-x", AgentName: "agent-x", ThreadID: thX.ID,
		Name: "tick", Prompt: "do", ScheduleKind: "every", ScheduleExpr: "1h",
		Enabled: true, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(&jobX).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.AgentCronRun{
		ID: "run-x", JobID: jobX.ID, Status: "ok", StartedAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	en := true
	agent := "agent-x"
	if _, err := pm.UpdateBinding(&en, &agent, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpsertMemory("agent-x", "XB", "keep", "agent", "x"); err != nil {
		t.Fatal(err)
	}

	if err := pm.PurgeAgentEverywhere("agent-x"); err != nil {
		t.Fatal(err)
	}

	memX, _ := pm.ListMemories("agent-x")
	if len(memX) != 0 {
		t.Fatalf("agent-x memories should be gone: %v", memX)
	}
	memY, _ := pm.ListMemories("agent-y")
	if len(memY) != 1 {
		t.Fatalf("agent-y memories must survive: %v", memY)
	}
	threadsX, _ := pm.ListThreadsForAgent("agent-x")
	if len(threadsX) != 0 {
		t.Fatalf("agent-x threads should be gone: %v", threadsX)
	}
	threadsY, _ := pm.ListThreadsForAgent("agent-y")
	if len(threadsY) != 1 || threadsY[0].ID != thY.ID {
		t.Fatalf("agent-y thread must survive: %v", threadsY)
	}
	var jobs int64
	db.Model(&models.AgentCronJob{}).Where("id = ?", "job-x").Count(&jobs)
	if jobs != 0 {
		t.Fatalf("job-x should be deleted")
	}
	b, err := pm.GetBinding()
	if err != nil {
		t.Fatal(err)
	}
	if b.Enabled || b.AgentConfigRef != "" {
		t.Fatalf("PM Leader should be unbound after purge: %+v", b)
	}
}

func TestUpdateMemoryForAgentScoped(t *testing.T) {
	db := setupPmDB(t)
	skills := NewAgentService(t.TempDir())
	pm := NewPmService(db, skills)
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("MemScope", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := pm.UpsertMemory("agent-a", "T", "old", "admin", "u")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pm.UpdateMemoryForAgent("agent-b", a.ID, "T2", "hijack", "u")
	if !errors.Is(err, ErrPmMemoryNotFound) {
		t.Fatalf("want not found for other agent, got %v", err)
	}
	got, err := pm.UpdateMemoryForAgent("agent-a", a.ID, "T2", "new", "u")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "new" || got.Title != "T2" {
		t.Fatalf("got %+v", got)
	}
}

func TestRenameAgentScopedData(t *testing.T) {
	db := setupPmDB(t)
	skills := NewAgentService(t.TempDir())
	pm := NewPmService(db, skills)
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("RenameHome", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpsertMemory("old-name", "T", "c", "admin", "u"); err != nil {
		t.Fatal(err)
	}
	if err := pm.RenameAgentScopedData("old-name", "new-name"); err != nil {
		t.Fatal(err)
	}
	mem, _ := pm.ListMemories("new-name")
	if len(mem) != 1 {
		t.Fatalf("renamed memories: %v", mem)
	}
	old, _ := pm.ListMemories("old-name")
	if len(old) != 0 {
		t.Fatalf("old name should be empty: %v", old)
	}
}
