package services

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupPmDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:pm_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestPmBindingEnableRequiresAgent(t *testing.T) {
	db := setupPmDB(t)
	skills := NewAgentService(t.TempDir())
	pm := NewPmService(db, skills)
	p, err := NewProjectService(db).Create("P1", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	en := true
	if _, err := pm.UpdateBinding(p.ID, &en, nil, nil, nil, nil); !errors.Is(err, ErrPmLeaderNoAgent) {
		t.Fatalf("want ErrPmLeaderNoAgent, got %v", err)
	}
	agent := "demo-agent"
	// without agent on disk still fails
	if _, err := pm.UpdateBinding(p.ID, &en, &agent, nil, nil, nil); !errors.Is(err, ErrPmLeaderNoAgent) {
		t.Fatalf("want ErrPmLeaderNoAgent for missing agent, got %v", err)
	}
}

func TestPmMemoryAndThreadIsolation(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	pm.SetBlobStore(blob.NewMemory())
	ps := NewProjectService(db)
	p, err := ps.Create("MemProj", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	item, err := pm.UpsertMemory(p.ID, "agent", "背景", "我们用 Go", "admin", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "背景" {
		t.Fatal(item)
	}
	items, err := pm.ListMemories(p.ID, "")
	if err != nil || len(items) != 1 {
		t.Fatalf("list=%v err=%v", items, err)
	}
	ta, err := pm.CreateThread(p.ID, "alice", "", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}
	tb, err := pm.CreateThread(p.ID, "bob", "", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.GetThread(p.ID, ta.ID, "bob"); !errors.Is(err, ErrPmThreadNotFound) {
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
	bobThreads, _ := pm.ListThreads(p.ID, "bob")
	if len(bobThreads) != 1 || bobThreads[0].ID != tb.ID {
		t.Fatalf("bob threads=%v", bobThreads)
	}
	n, err := pm.ClearMemoriesForAgent(p.ID, "agent")
	if err != nil || n != 1 {
		t.Fatalf("clear n=%d err=%v", n, err)
	}
}

func TestPmRequireEnabled(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("EnProj", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.RequireEnabled(p.ID); !errors.Is(err, ErrPmLeaderDisabled) {
		t.Fatalf("got %v", err)
	}
	agent := "x"
	en := true
	// skills nil → agent existence skipped
	if _, err := pm.UpdateBinding(p.ID, &en, &agent, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.RequireEnabled(p.ID); err != nil {
		t.Fatal(err)
	}
	dis := false
	if _, err := pm.UpdateBinding(p.ID, &dis, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.RequireEnabled(p.ID); !errors.Is(err, ErrPmLeaderDisabled) {
		t.Fatalf("got %v", err)
	}
}

func TestPmMessageFailureAndRecentFilter(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("FailProj", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateThread(p.ID, "alice", "", "agent", "user")
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
	ps := NewProjectService(db)
	p, err := ps.Create("DraftProj", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	th, err := pm.CreateThread(p.ID, "alice", "", "agent", "user")
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
	ps := NewProjectService(db)
	p, err := ps.Create("IsoMem", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := pm.UpsertMemory(p.ID, "agent-a", "A记", "secret-a", "agent", "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := pm.UpsertMemory(p.ID, "agent-b", "B记", "secret-b", "agent", "b")
	if err != nil {
		t.Fatal(err)
	}
	onlyA, err := pm.ListMemories(p.ID, "agent-a")
	if err != nil || len(onlyA) != 1 || onlyA[0].ID != a.ID {
		t.Fatalf("agent-a list=%v err=%v", onlyA, err)
	}
	if err := pm.DeleteMemoryForAgent(p.ID, "agent-a", b.ID); !errors.Is(err, ErrPmMemoryNotFound) {
		t.Fatalf("cross-agent delete should fail: %v", err)
	}
	if _, err := pm.GetMemory(p.ID, "agent-a", b.ID); !errors.Is(err, ErrPmMemoryNotFound) {
		t.Fatalf("cross-agent get should fail: %v", err)
	}
	if err := pm.DeleteMemoryForAgent(p.ID, "agent-a", a.ID); err != nil {
		t.Fatal(err)
	}
	left, _ := pm.ListMemories(p.ID, "")
	if len(left) != 1 || left[0].ID != b.ID {
		t.Fatalf("after scoped delete left=%v", left)
	}

	n, err := pm.ClearMemoriesForAgent(p.ID, "agent-b")
	if err != nil || n != 1 {
		t.Fatalf("clear agent-b n=%d err=%v", n, err)
	}
	all, _ := pm.ListMemories(p.ID, "")
	if len(all) != 0 {
		t.Fatalf("pm memories left=%v", all)
	}
}

func TestNormalizePmEnabledMcps(t *testing.T) {
	if got := EffectivePmEnabledMcps(nil); len(got) != 5 {
		t.Fatalf("nil default=%v", got)
	}
	got := FilterPmEnabledMcps([]string{"pm-workflow-read", "memory-store", "pm-workflow-read", "pm-progress", "pm-agent-fs", "pm-prd-manager"})
	if len(got) != 4 || got[0] != "pm-workflow-read" || got[1] != "pm-progress" || got[2] != "pm-agent-fs" || got[3] != "pm-prd-manager" {
		t.Fatalf("filtered=%v", got)
	}
	got = EffectivePmEnabledMcps([]string{"memory-store", "task-scheduler"})
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
	ps := NewProjectService(db)
	p, err := ps.Create("EmptyMcps", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	en := true
	agent := "agent-a"
	b, err := pm.UpdateBinding(p.ID, &en, &agent, []string{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.EnabledMcps) != 0 {
		t.Fatalf("update want empty, got %v", b.EnabledMcps)
	}
	b2, err := pm.GetBinding(p.ID)
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
	ps := NewProjectService(db)
	p, err := ps.Create("SearchMem", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpsertMemory(p.ID, "agent-a", "A记", "alpha-keyword", "agent", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpsertMemory(p.ID, "agent-b", "B记", "alpha-keyword", "agent", "b"); err != nil {
		t.Fatal(err)
	}
	hits, err := pm.SearchMemories(p.ID, "agent-a", "alpha", 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%v err=%v", hits, err)
	}
	if hits[0]["title"] != "A记" {
		t.Fatalf("hit=%v", hits[0])
	}
}

func TestPmChannelThreadListMergeAndACL(t *testing.T) {
	db := setupPmDB(t)
	pm := NewPmService(db, nil)
	ps := NewProjectService(db)
	p, err := ps.Create("ChProj", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	base := time.Now().UTC().Truncate(time.Second)
	webOlder, err := pm.CreateThread(p.ID, "alice", "Web 旧", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}
	guild, err := pm.CreateThread(p.ID, "qq:guild:ch1", "频道会话", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}
	group, err := pm.CreateThread(p.ID, "qq:group:g1", "群会话", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}
	c2c, err := pm.CreateThread(p.ID, "qq:c2c:u1", "   ", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}
	wecom, err := pm.CreateThread(p.ID, "wecom:c2c:zhangsan", "企微会话", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pm.CreateCronThread(p.ID, "agent", "cron job"); err != nil {
		t.Fatal(err)
	}
	webNewer, err := pm.CreateThread(p.ID, "alice", "Web 新", "agent", "user")
	if err != nil {
		t.Fatal(err)
	}

	// Explicit timestamps: webNewer > guild > webOlder (mixed order).
	stamps := map[string]time.Time{
		webNewer.ID: base.Add(3 * time.Hour),
		guild.ID:    base.Add(2 * time.Hour),
		group.ID:    base.Add(90 * time.Minute),
		c2c.ID:      base.Add(time.Hour),
		wecom.ID:    base.Add(50 * time.Minute),
		webOlder.ID: base,
	}
	for id, ts := range stamps {
		if err := db.Model(&models.ChatThread{}).Where("id = ?", id).Update("updated_at", ts).Error; err != nil {
			t.Fatal(err)
		}
	}

	listed, err := pm.ListThreads(p.ID, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 6 {
		t.Fatalf("want 2 web + 4 channel (no cron), got %d: %+v", len(listed), listed)
	}
	foundWecom := false
	for _, th := range listed {
		if th.UserID == "wecom:c2c:zhangsan" {
			foundWecom = true
			if !th.Unspoken {
				t.Fatal("wecom thread without channel inbound must be unspoken")
			}
		}
	}
	if !foundWecom {
		t.Fatal("wecom thread missing from list")
	}
	if listed[0].ID != webNewer.ID || listed[1].ID != guild.ID || listed[5].ID != webOlder.ID {
		ids := make([]string, len(listed))
		for i, th := range listed {
			ids[i] = th.ID
		}
		t.Fatalf("mixed updated_at desc order wrong: %v", ids)
	}
	for _, th := range listed {
		if strings.HasPrefix(th.UserID, "cron:") {
			t.Fatalf("cron must not appear: %+v", th)
		}
	}

	// Synthetic qq: lookup stays owner-only (ChannelBridge).
	onlyGuild, err := pm.ListThreads(p.ID, "qq:guild:ch1")
	if err != nil || len(onlyGuild) != 1 || onlyGuild[0].ID != guild.ID {
		t.Fatalf("synthetic list=%+v err=%v", onlyGuild, err)
	}

	// Bob (other member) can read channel threads.
	got, err := pm.GetThread(p.ID, guild.ID, "bob")
	if err != nil || got.ID != guild.ID {
		t.Fatalf("bob read channel: %+v err=%v", got, err)
	}
	// Bob cannot read alice web thread.
	if _, err := pm.GetThread(p.ID, webNewer.ID, "bob"); !errors.Is(err, ErrPmThreadNotFound) {
		t.Fatalf("bob must not read alice web: %v", err)
	}

	// Write/delete rejected for channel.
	if _, err := pm.RequireWritableThread(p.ID, guild.ID, "alice"); !errors.Is(err, ErrPmChannelReadOnly) {
		t.Fatalf("want ErrPmChannelReadOnly, got %v", err)
	}
	if err := pm.DeleteThread(p.ID, guild.ID, "alice"); !errors.Is(err, ErrPmChannelReadOnly) {
		t.Fatalf("delete channel: %v", err)
	}
	if _, err := pm.GetThreadByID(guild.ID); err != nil {
		t.Fatal("channel thread must still exist after rejected delete")
	}

	// Own web thread remains writable/deletable.
	if _, err := pm.RequireWritableThread(p.ID, webNewer.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := pm.DeleteThread(p.ID, webNewer.ID, "alice"); err != nil {
		t.Fatal(err)
	}
}

func TestIsChannelUserIDPrefixes(t *testing.T) {
	if !IsChannelUserID("qq:guild:x") || !IsChannelUserID("qq:group:y") || !IsChannelUserID("qq:c2c:z") {
		t.Fatal("expected qq: prefixes")
	}
	if !IsChannelUserID("wecom:c2c:zhangsan") || !IsChannelUserID("wecom:group:wr") {
		t.Fatal("expected wecom: prefixes")
	}
	if !IsChannelUserID("feishu:c2c:oc_x") || !IsChannelUserID("feishu:group:oc_g") {
		t.Fatal("expected feishu: prefixes to match")
	}
	if !IsChannelUserID("feishu:c2c:oc_x") {
		t.Fatal("IsChannelUserID should cover feishu")
	}
	if !IsChannelUserID("dingtalk:c2c:staff1") || !IsChannelUserID("dingtalk:group:cid") {
		t.Fatal("expected dingtalk: prefixes to match")
	}
	if !IsChannelUserID("dingtalk:group:cid") {
		t.Fatal("IsChannelUserID should cover dingtalk")
	}
	if IsChannelUserID("cron:agent") || IsChannelUserID("alice") || IsChannelUserID("") {
		t.Fatal("unexpected channel match")
	}
	if ChannelPeerID("wecom:c2c:zhangsan") != "zhangsan" {
		t.Fatalf("peer=%q", ChannelPeerID("wecom:c2c:zhangsan"))
	}
}
