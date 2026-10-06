package services

import (
	"errors"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestPurgeAgentProjectData(t *testing.T) {
	db := setupPmDB(t)
	skills := NewAgentService(t.TempDir())
	pm := NewPmService(db, skills)
	ps := NewProjectService(db)
	pA, err := ps.Create("ProjA", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	pB, err := ps.Create("ProjB", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := skills.Save(Agent{AcpBackend: AcpBackendCursor, Name: "agent-x", ProjectID: pA.ID}); err != nil {
		t.Fatal(err)
	}
	if err := skills.Save(Agent{AcpBackend: AcpBackendCursor, Name: "agent-y", ProjectID: pA.ID}); err != nil {
		t.Fatal(err)
	}

	if _, err := pm.UpsertMemory(pA.ID, "agent-x", "X记", "x-content", "agent", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpsertMemory(pA.ID, "agent-y", "Y记", "y-content", "agent", "y"); err != nil {
		t.Fatal(err)
	}
	thX, err := pm.CreateThread(pA.ID, "alice", "x-thread", "agent-x", models.ChatThreadKindUser)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.AppendMessage(thX.ID, "user", "hello", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	thY, err := pm.CreateThread(pA.ID, "bob", "y-thread", "agent-y", models.ChatThreadKindUser)
	if err != nil {
		t.Fatal(err)
	}
	jobX := models.AgentCronJob{
		ID: "job-x", AgentName: "agent-x", ProjectID: pA.ID, ThreadID: thX.ID,
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
	if _, err := pm.UpdateBinding(pA.ID, &en, &agent, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpsertMemory(pB.ID, "agent-x", "XB", "keep", "agent", "x"); err != nil {
		t.Fatal(err)
	}

	if err := pm.PurgeAgentProjectData(pA.ID, "agent-x"); err != nil {
		t.Fatal(err)
	}

	memA, _ := pm.ListMemories(pA.ID, "agent-x")
	if len(memA) != 0 {
		t.Fatalf("agent-x memories under A should be gone: %v", memA)
	}
	memY, _ := pm.ListMemories(pA.ID, "agent-y")
	if len(memY) != 1 {
		t.Fatalf("agent-y memories must survive: %v", memY)
	}
	memB, _ := pm.ListMemories(pB.ID, "agent-x")
	if len(memB) != 1 {
		t.Fatalf("agent-x memories under B must survive: %v", memB)
	}
	threadsX, _ := pm.ListThreadsForAgent(pA.ID, "agent-x")
	if len(threadsX) != 0 {
		t.Fatalf("agent-x threads under A should be gone: %v", threadsX)
	}
	threadsY, _ := pm.ListThreadsForAgent(pA.ID, "agent-y")
	if len(threadsY) != 1 || threadsY[0].ID != thY.ID {
		t.Fatalf("agent-y thread must survive: %v", threadsY)
	}
	var jobs int64
	db.Model(&models.AgentCronJob{}).Where("id = ?", "job-x").Count(&jobs)
	if jobs != 0 {
		t.Fatalf("job-x should be deleted")
	}
	b, err := pm.GetBinding(pA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Enabled || b.AgentConfigRef != "" {
		t.Fatalf("PM Leader should be unbound after purge: %+v", b)
	}
}

func TestUpdateBindingRequiresAgentHomeProject(t *testing.T) {
	db := setupPmDB(t)
	skills := NewAgentService(t.TempDir())
	pm := NewPmService(db, skills)
	ps := NewProjectService(db)
	pA, err := ps.Create("BindA", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	pB, err := ps.Create("BindB", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := skills.Save(Agent{AcpBackend: AcpBackendCursor, Name: "home-b", ProjectID: pB.ID}); err != nil {
		t.Fatal(err)
	}
	en := true
	agent := "home-b"
	if _, err := pm.UpdateBinding(pA.ID, &en, &agent, nil, nil, nil); !errors.Is(err, ErrPmLeaderProjectMismatch) {
		t.Fatalf("want ErrPmLeaderProjectMismatch, got %v", err)
	}
	if err := skills.Save(Agent{AcpBackend: AcpBackendCursor, Name: "home-a", ProjectID: pA.ID}); err != nil {
		t.Fatal(err)
	}
	agent = "home-a"
	if _, err := pm.UpdateBinding(pA.ID, &en, &agent, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateMemoryForAgentScoped(t *testing.T) {
	db := setupPmDB(t)
	skills := NewAgentService(t.TempDir())
	pm := NewPmService(db, skills)
	ps := NewProjectService(db)
	p, err := ps.Create("MemScope", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := pm.UpsertMemory(p.ID, "agent-a", "T", "old", "admin", "u")
	if err != nil {
		t.Fatal(err)
	}
	_, err = pm.UpdateMemoryForAgent(p.ID, "agent-b", a.ID, "T2", "hijack", "u")
	if !errors.Is(err, ErrPmMemoryNotFound) {
		t.Fatalf("want not found for other agent, got %v", err)
	}
	got, err := pm.UpdateMemoryForAgent(p.ID, "agent-a", a.ID, "T2", "new", "u")
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
	ps := NewProjectService(db)
	p, err := ps.Create("RenameHome", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpsertMemory(p.ID, "old-name", "T", "c", "admin", "u"); err != nil {
		t.Fatal(err)
	}
	if err := pm.RenameAgentScopedData("old-name", "new-name"); err != nil {
		t.Fatal(err)
	}
	mem, _ := pm.ListMemories(p.ID, "new-name")
	if len(mem) != 1 {
		t.Fatalf("renamed memories: %v", mem)
	}
	old, _ := pm.ListMemories(p.ID, "old-name")
	if len(old) != 0 {
		t.Fatalf("old name should be empty: %v", old)
	}
}

func TestAgentProjectMatches(t *testing.T) {
	if !AgentProjectMatches(Agent{AcpBackend: AcpBackendCursor, ProjectID: "p1"}, "p1") {
		t.Fatal("expected match")
	}
	if AgentProjectMatches(Agent{AcpBackend: AcpBackendCursor, ProjectID: "p1"}, "p2") {
		t.Fatal("expected mismatch")
	}
}
