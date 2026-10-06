package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/config"
)

func TestPmThreadDeleteAndLookupHelpers(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("DelThread", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateThread(p.ID, "alice", "t1", "agent-a", "user")
	if err != nil {
		t.Fatal(err)
	}
	got, err := pm.GetThreadByID(th.ID)
	if err != nil || got.ID != th.ID {
		t.Fatalf("GetThreadByID=%+v err=%v", got, err)
	}
	if err := pm.BindSandbox(th.ID, 42); err != nil {
		t.Fatal(err)
	}
	if err := pm.ClearSandboxRef(th.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.AppendMessage(th.ID, "user", "one", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	counts, err := pm.CountMessagesByThreads([]string{th.ID, "missing"})
	if err != nil || counts[th.ID] != 1 {
		t.Fatalf("counts=%v err=%v", counts, err)
	}
	if err := pm.DeleteThread(p.ID, th.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.GetThreadByID(th.ID); err == nil {
		t.Fatal("thread should be gone after DeleteThread")
	}
}

func TestPmDeleteThreadByID(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("DelByID", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateCronThread(p.ID, "agent-a", "cron")
	if err != nil {
		t.Fatal(err)
	}
	if err := pm.DeleteThreadByID(th.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.GetThreadByID(th.ID); err == nil {
		t.Fatal("thread should be gone")
	}
}

func TestBuildAgentAndPmMCPSpecs(t *testing.T) {
	prev := config.GetConfig()
	t.Cleanup(func() { config.StoreConfig(prev) })
	config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "http://localhost:8080"}})

	specs := BuildAgentPlatformMCPSpecs("proj-1", "agent-a", "tok-1")
	if len(specs) == 0 {
		t.Fatal("agent platform specs")
	}
	pmSpecs := BuildPmRoleMCPSpecs("proj-1", "tok-1", []string{"pm-workflow-read", "pm-progress"})
	if len(pmSpecs) < 2 {
		t.Fatalf("pm role specs=%v", pmSpecs)
	}
	all := append(BuildAgentPlatformMCPSpecs("proj-1", "agent-a", "tok-1"), BuildPmRoleMCPSpecs("proj-1", "tok-1", []string{"pm-workflow-read"})...)
	if len(all) == 0 {
		t.Fatal("pm platform specs")
	}
}

func TestEffectivePmEnabledMcpsDirect(t *testing.T) {
	got := EffectivePmEnabledMcps([]string{"pm-workflow-read", "bogus"})
	if len(got) != 1 || got[0] != "pm-workflow-read" {
		t.Fatalf("effective=%v", got)
	}
}

func TestPmUpsertDraftBranches(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("DraftBranch", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateThread(p.ID, "alice", "", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}
	user, err := pm.AppendMessage(th.ID, "user", "q", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	d1, err := pm.UpsertDraft(th.ID, user.ID, "a", PmDraftStreaming, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := pm.UpsertDraft(th.ID, user.ID, "b", PmDraftStreaming, 2, 1, 0)
	if err != nil || d2.PartialText != "b" {
		t.Fatalf("update draft=%+v err=%v", d2, err)
	}
	_ = d1
}
