package services

import (
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/filestore"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupPmDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:pm_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := filestore.Attach(db, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	return db
}

func TestPmBindingEnableRequiresAgent(t *testing.T) {
	db := setupPmDB(t)
	skills := NewAgentService(t.TempDir())
	pm := NewPmService(db, skills)
	_, err := NewWorkspaceService(db).CreateFixture("P1", "")
	if err != nil {
		t.Fatal(err)
	}
	en := true
	if _, err := pm.UpdateBinding(&en, nil, nil, nil, nil); !errors.Is(err, ErrPmLeaderNoAgent) {
		t.Fatalf("want ErrPmLeaderNoAgent, got %v", err)
	}
	agent := "demo-agent"
	// without agent on disk still fails
	if _, err := pm.UpdateBinding(&en, &agent, nil, nil, nil); !errors.Is(err, ErrPmLeaderNoAgent) {
		t.Fatalf("want ErrPmLeaderNoAgent for missing agent, got %v", err)
	}
}

func TestPmMemoryAndThreadIsolation(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	pm.SetBlobStore(blob.NewMemory())
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("MemProj", "")
	if err != nil {
		t.Fatal(err)
	}
	item, err := pm.UpsertMemory("agent", "背景", "我们用 Go", "admin", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "背景" {
		t.Fatal(item)
	}
	items, err := pm.ListMemories("")
	if err != nil || len(items) != 1 {
		t.Fatalf("list=%v err=%v", items, err)
	}
	ta, err := pm.CreateThread("alice", "", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}
	tb, err := pm.CreateThread("bob", "", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.GetThread(ta.ID, "bob"); !errors.Is(err, ErrPmThreadNotFound) {
		t.Fatalf("bob must not see alice thread: %v", err)
	}
	if _, err := pm.AppendMessage(ta.ID, "user", "进度如何？", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	msgs, err := pm.ListMessages(ta.ID)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("msgs=%v err=%v", msgs, err)
	}
	imgMsg, err := pm.AppendMessage(ta.ID, "user", "", nil, nil, []models.PromptImage{
		{Data: "YWJj", MimeType: "image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(imgMsg.Images) != 1 || imgMsg.Images[0].MimeType != "image/png" || imgMsg.Images[0].Ref == "" || imgMsg.Images[0].Data != "" {
		t.Fatalf("images not externalized: %+v", imgMsg)
	}
	msgs, err = pm.ListMessages(ta.ID)
	if err != nil || len(msgs) != 2 {
		t.Fatalf("msgs after image=%v err=%v", msgs, err)
	}
	if len(msgs[1].Images) != 1 || msgs[1].Images[0].Ref == "" || msgs[1].Images[0].Data != "" {
		t.Fatalf("listed message missing blob ref: %+v", msgs[1])
	}
	bobThreads, _ := pm.ListThreads("bob")
	if len(bobThreads) != 1 || bobThreads[0].ID != tb.ID {
		t.Fatalf("bob threads=%v", bobThreads)
	}
	n, err := pm.ClearMemoriesForAgent("agent")
	if err != nil || n != 1 {
		t.Fatalf("clear n=%d err=%v", n, err)
	}
}

func TestPmRequireEnabled(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("EnProj", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.RequireEnabled(); !errors.Is(err, ErrPmLeaderDisabled) {
		t.Fatalf("got %v", err)
	}
	agent := "x"
	en := true
	// skills nil → agent existence skipped
	if _, err := pm.UpdateBinding(&en, &agent, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.RequireEnabled(); err != nil {
		t.Fatal(err)
	}
	dis := false
	if _, err := pm.UpdateBinding(&dis, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.RequireEnabled(); !errors.Is(err, ErrPmLeaderDisabled) {
		t.Fatalf("got %v", err)
	}
}

func TestPmMessageFailureAndRecentFilter(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("FailProj", "")
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateThread("alice", "", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}
	okUser, err := pm.AppendMessage(th.ID, "user", "先问一句", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if okUser.Status != "ok" {
		t.Fatalf("default status=%q", okUser.Status)
	}
	_, err = pm.AppendMessage(th.ID, "assistant", "先答一句", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	failUser, err := pm.AppendMessage(th.ID, "user", "失败轮次", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	marked, err := pm.UpdateMessageFailure(th.ID, failUser.ID, "failed", PmFailEmpty)
	if err != nil {
		t.Fatal(err)
	}
	if marked.Status != "failed" || marked.FailKind != PmFailEmpty {
		t.Fatalf("marked=%+v", marked)
	}
	if _, err := pm.UpdateMessageFailure(th.ID, failUser.ID, "failed", "bogus"); !errors.Is(err, ErrPmMessageInvalidStatus) {
		t.Fatalf("want invalid status, got %v", err)
	}
	recent, err := pm.RecentMessages(th.ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range recent {
		if m.ID == failUser.ID || m.Status == "failed" {
			t.Fatalf("failed message leaked into recent: %+v", m)
		}
	}
	if len(recent) != 2 {
		t.Fatalf("recent want 2 got %d", len(recent))
	}
	cleared, err := pm.UpdateMessageFailure(th.ID, failUser.ID, "ok", "")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Status != "ok" || cleared.FailKind != "" {
		t.Fatalf("cleared=%+v", cleared)
	}
	recent, err = pm.RecentMessages(th.ID, 20)
	if err != nil || len(recent) != 3 {
		t.Fatalf("after clear recent=%d err=%v", len(recent), err)
	}
	asst, err := pm.AppendMessage(th.ID, "assistant", "不应可标失败", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpdateMessageFailure(th.ID, asst.ID, "failed", PmFailUnknown); !errors.Is(err, ErrPmMessageInvalidRole) {
		t.Fatalf("want invalid role, got %v", err)
	}
}

func TestPmDraftCheckpoint(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("DraftProj", "")
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateThread("alice", "", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}
	user, err := pm.AppendMessage(th.ID, "user", "问进度", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := pm.UpsertDraft(th.ID, user.ID, "partial", PmDraftStreaming, 1, 0, 9)
	if err != nil {
		t.Fatal(err)
	}
	if d.PartialText != "partial" || d.UserMsgID != user.ID {
		t.Fatalf("draft=%+v", d)
	}
	got, err := pm.GetDraft(th.ID)
	if err != nil || got == nil || got.Status != PmDraftStreaming {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	n, err := pm.FailInterruptedTurns()
	if err != nil || n != 1 {
		t.Fatalf("sweep n=%d err=%v", n, err)
	}
	if got, _ = pm.GetDraft(th.ID); got == nil || got.Status != PmDraftFailed || got.FailKind != PmFailInterrupted {
		t.Fatalf("draft after sweep=%+v", got)
	}
	if m, _ := pm.GetMessage(th.ID, user.ID); m.Status != "failed" || m.FailKind != PmFailInterrupted {
		t.Fatalf("user msg after sweep=%+v", m)
	}
	if n, _ = pm.FailInterruptedTurns(); n != 0 {
		t.Fatalf("second sweep n=%d", n)
	}
	has, err := pm.HasAssistantAfter(th.ID, user.ID)
	if err != nil || has {
		t.Fatalf("has=%v err=%v", has, err)
	}
	if _, err := pm.AppendMessage(th.ID, "assistant", "终稿", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	has, err = pm.HasAssistantAfter(th.ID, user.ID)
	if err != nil || !has {
		t.Fatalf("has final want true got %v err=%v", has, err)
	}
	if err := pm.ClearDraft(th.ID); err != nil {
		t.Fatal(err)
	}
	got, err = pm.GetDraft(th.ID)
	if err != nil || got != nil {
		t.Fatalf("cleared draft=%+v err=%v", got, err)
	}
}

func TestPmMemoryAgentIsolation(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("IsoMem", "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := pm.UpsertMemory("agent-a", "A记", "secret-a", "agent", "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := pm.UpsertMemory("agent-b", "B记", "secret-b", "agent", "b")
	if err != nil {
		t.Fatal(err)
	}
	onlyA, err := pm.ListMemories("agent-a")
	if err != nil || len(onlyA) != 1 || onlyA[0].ID != a.ID {
		t.Fatalf("agent-a list=%v err=%v", onlyA, err)
	}
	if err := pm.DeleteMemoryForAgent("agent-a", b.ID); !errors.Is(err, ErrPmMemoryNotFound) {
		t.Fatalf("cross-agent delete should fail: %v", err)
	}
	if _, err := pm.GetMemory("agent-a", b.ID); !errors.Is(err, ErrPmMemoryNotFound) {
		t.Fatalf("cross-agent get should fail: %v", err)
	}
	if err := pm.DeleteMemoryForAgent("agent-a", a.ID); err != nil {
		t.Fatal(err)
	}
	left, _ := pm.ListMemories("")
	if len(left) != 1 || left[0].ID != b.ID {
		t.Fatalf("after scoped delete left=%v", left)
	}

	n, err := pm.ClearMemoriesForAgent("agent-b")
	if err != nil || n != 1 {
		t.Fatalf("clear agent-b n=%d err=%v", n, err)
	}
	all, _ := pm.ListMemories("")
	if len(all) != 0 {
		t.Fatalf("pm memories left=%v", all)
	}
}

func TestNormalizePmEnabledMcps(t *testing.T) {
	if got := EffectivePmEnabledMcps(nil); len(got) != 4 {
		t.Fatalf("nil default=%v", got)
	}
	got := FilterPmEnabledMcps([]string{"pm-workflow-read", "memory-store", "pm-workflow-read", "pm-progress", "pm-agent-fs", "pm-prd-manager"})
	if len(got) != 3 || got[0] != "pm-workflow-read" || got[1] != "pm-progress" || got[2] != "pm-agent-fs" {
		t.Fatalf("filtered=%v", got)
	}
	got = EffectivePmEnabledMcps([]string{"memory-store", "context-store"})
	if len(got) != 0 {
		t.Fatalf("explicit all-invalid should be empty, got %v", got)
	}
	got = EffectivePmEnabledMcps([]string{})
	if len(got) != 0 {
		t.Fatalf("explicit empty should stay empty, got %v", got)
	}
}

func TestUpdateBindingEmptyEnabledMcpsPersists(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("EmptyMcps", "")
	if err != nil {
		t.Fatal(err)
	}
	en := true
	agent := "agent-a"
	b, err := pm.UpdateBinding(&en, &agent, []string{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.EnabledMcps) != 0 {
		t.Fatalf("update want empty, got %v", b.EnabledMcps)
	}
	b2, err := pm.GetBinding()
	if err != nil {
		t.Fatal(err)
	}
	if len(b2.EnabledMcps) != 0 {
		t.Fatalf("get want empty after persist, got %v", b2.EnabledMcps)
	}
}

func TestPmMemorySearchAndBackfillLegacy(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewWorkspaceService(db)
	_, err := ps.CreateFixture("SearchMem", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpsertMemory("agent-a", "A记", "alpha-keyword", "agent", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpsertMemory("agent-b", "B记", "alpha-keyword", "agent", "b"); err != nil {
		t.Fatal(err)
	}
	hits, err := pm.SearchMemories("agent-a", "alpha", 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	if hits[0]["title"] != "A记" {
		t.Fatalf("hit=%v", hits[0])
	}
}
