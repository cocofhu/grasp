package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

func seedSoloMessages(t *testing.T, db *gorm.DB, sessionID string, at time.Time, rows ...models.SoloMessage) {
	t.Helper()
	for i := range rows {
		rows[i].SessionID = sessionID
		if rows[i].CreatedAt.IsZero() {
			rows[i].CreatedAt = at.Add(time.Duration(i) * time.Second)
		}
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadReferenceUsesPinnedSnapshotAndOwnCursor(t *testing.T) {
	svc, db, sb := conversationHistoryFixture(t)
	at := time.Date(2026, 10, 28, 1, 0, 0, 0, time.UTC)
	for _, s := range []models.SoloSession{
		{ID: "other", UserID: "alice", Title: "登录重构", Status: models.SoloStatusClosed},
		{ID: "empty", UserID: "alice", Title: "空会话", Status: models.SoloStatusClosed},
		{ID: "bobs", UserID: "bob", Title: "别人的", Status: models.SoloStatusRunning},
	} {
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
	seedSoloMessages(t, db, "other", at,
		models.SoloMessage{ID: "o1", Role: "user", Content: "要求：保留圆角"},
		models.SoloMessage{ID: "o2", Role: "assistant", Content: "好的"},
		models.SoloMessage{ID: "o3", Role: "user", Content: "再改成直角"},
		models.SoloMessage{ID: "o4", Role: "user", Content: "引用之后的新消息"},
	)
	refs := []models.MessageReference{
		{ID: "ref-solo", Kind: models.ReferenceKindSolo, Target: "other", Title: "登录重构", Anchor: "o3", Messages: 3},
		{ID: "ref-empty", Kind: models.ReferenceKindSolo, Target: "empty", Title: "空会话"},
		{ID: "ref-gh", Kind: models.ReferenceKindGitHubPR, Target: "acme/web#7"},
		{ID: "ref-bob", Kind: models.ReferenceKindSolo, Target: "bobs", Anchor: "b1"},
		{ID: "ref_%", Kind: models.ReferenceKindSolo, Target: "other"},
	}
	seedSoloMessages(t, db, sb.ThreadID, at, models.SoloMessage{ID: "c1", Role: "user", Content: "看 @登录重构", References: refs})
	// A non-user row naming the ref must not attach it.
	seedSoloMessages(t, db, sb.ThreadID, at.Add(time.Minute), models.SoloMessage{ID: "c2", Role: "assistant", Content: "x", References: []models.MessageReference{{ID: "ref-fake", Kind: models.ReferenceKindSolo, Target: "other"}}})

	first, err := svc.ReadReference(sb.RunID, "ref-solo", mcp.ConversationHistoryRequest{View: "messages", Limit: 2})
	if err != nil || first.Ref != "ref-solo" || first.ConversationID != "other" || len(first.Messages) != 2 || first.Messages[1].ID != "o3" || !first.HasMore {
		t.Fatalf("first page: %+v %v", first, err)
	}
	second, err := svc.ReadReference(sb.RunID, "ref-solo", mcp.ConversationHistoryRequest{View: "summary", Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(second.Messages) != 1 || second.Messages[0].ID != "o1" || !strings.Contains(second.Summary, "保留圆角") || strings.Contains(second.Summary, "新消息") {
		t.Fatalf("second page leaked past the anchor: %+v %v", second, err)
	}
	if _, err := svc.ReadReference(sb.RunID, "ref-empty", mcp.ConversationHistoryRequest{Cursor: first.NextCursor}); err == nil {
		t.Fatal("cursor reused across references")
	}
	own, _ := svc.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{View: "messages", Limit: 1})
	if _, err := svc.ReadReference(sb.RunID, "ref-solo", mcp.ConversationHistoryRequest{Cursor: own.HistoryVersion}); err == nil {
		t.Fatal("v1 history cursor accepted for a reference")
	}
	if _, err := svc.ReadConversationHistory(sb.RunID, mcp.ConversationHistoryRequest{Cursor: first.NextCursor}); err == nil {
		t.Fatal("reference cursor accepted for own history")
	}
	if empty, err := svc.ReadReference(sb.RunID, "ref-empty", mcp.ConversationHistoryRequest{}); err != nil || len(empty.Messages) != 0 || !strings.Contains(empty.Summary, "尚无历史") {
		t.Fatalf("empty reference: %+v %v", empty, err)
	}
	var hint *mcp.ReferenceHintError
	if _, err := svc.ReadReference(sb.RunID, "ref-gh", mcp.ConversationHistoryRequest{}); !errors.As(err, &hint) || !strings.Contains(hint.Hint, "gh pr view 7") {
		t.Fatalf("github hint: %v", err)
	}
	for _, ref := range []string{"ref-bob", "ref-fake", "ref-missing", "", "ref"} {
		if _, err := svc.ReadReference(sb.RunID, ref, mcp.ConversationHistoryRequest{}); err == nil {
			t.Fatalf("%q readable", ref)
		}
	}
	if _, err := svc.ReadReference("other-run", "ref-solo", mcp.ConversationHistoryRequest{}); err == nil {
		t.Fatal("foreign run")
	}
	// A deleted target stops being readable even with a valid handle.
	db.Delete(&models.SoloSession{ID: "other"})
	if _, err := svc.ReadReference(sb.RunID, "ref-solo", mcp.ConversationHistoryRequest{}); err == nil || !strings.Contains(err.Error(), "已被删除") {
		t.Fatalf("deleted target: %v", err)
	}
	// Closed or idle sessions still serve their history to references.
	db.Model(&models.SoloSession{}).Where("id = ?", "empty").Update("status", models.SoloStatusClosed)
	if _, err := svc.ReadReference(sb.RunID, "ref-empty", mcp.ConversationHistoryRequest{}); err != nil {
		t.Fatalf("closed target: %v", err)
	}
}

func referenceFixture(t *testing.T) (*SoloService, *gorm.DB, *fakeGitHub, string) {
	t.Helper()
	acp, port := fakeACPServer(t)
	t.Cleanup(acp.Close)
	solo, _, _, db := newSoloFixture(t, port, 3)
	gh, srv := newFakeGitHub(t)
	solo.SetGitHubAPIForTest(srv.URL, srv.Client())
	p, err := solo.Projects().Create(ProjectInput{Name: "web", RepoURL: "https://github.com/acme/web.git", DefaultBranch: "main"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA", ProjectID: p.ID})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = solo.Close(context.Background(), view.ID, "alice") })
	waitFor(t, "running session", func() bool {
		row, err := solo.Require(view.ID, "alice")
		return err == nil && row.Status == models.SoloStatusRunning
	})
	return solo, db, gh, view.ID
}

func TestSoloSendPinsReferencesWhenTheTurnBegins(t *testing.T) {
	solo, db, _, id := referenceFixture(t)
	solo.SetModelAlignerForTest(&recordAligner{})
	at := time.Now().Add(-time.Hour)
	for _, s := range []models.SoloSession{
		{ID: "older", UserID: "alice", Title: "登录重构", Status: models.SoloStatusClosed},
		{ID: "bobs", UserID: "bob", Title: "别人的", Status: models.SoloStatusClosed},
	} {
		if err := db.Create(&s).Error; err != nil {
			t.Fatal(err)
		}
	}
	seedSoloMessages(t, db, "older", at, models.SoloMessage{ID: "x1", Role: "user", Content: "要求：保留圆角"})

	for _, tc := range []struct {
		refs []ReferenceInput
		code string
	}{
		{[]ReferenceInput{{Kind: "solo", Target: id}}, "self_reference"},
		{[]ReferenceInput{{Kind: "solo", Target: "bobs"}}, "reference_not_found"},
		{[]ReferenceInput{{Kind: "solo", Target: ""}}, "reference_not_found"},
		{[]ReferenceInput{{Kind: "jira", Target: "X-1"}}, "reference_kind_invalid"},
		{[]ReferenceInput{{Kind: "github_issue", Target: "#3"}}, "reference_target_invalid"},
		{[]ReferenceInput{{Kind: "github_issue", Target: "a/b#1"}, {Kind: "github_issue", Target: "a/b#2"}, {Kind: "github_issue", Target: "a/b#3"}, {Kind: "github_issue", Target: "a/b#4"}, {Kind: "github_issue", Target: "a/b#5"}, {Kind: "github_issue", Target: "a/b#6"}}, "reference_limit"},
	} {
		_, err := solo.SendWithReferences(id, "alice", "x", nil, tc.refs)
		var se *SoloError
		if !errors.As(err, &se) || se.Code != tc.code {
			t.Fatalf("%v: got %v want %s", tc.refs, err, tc.code)
		}
	}
	if _, err := solo.SendWithReferences(id, "alice", "  ", nil, nil); err == nil {
		t.Fatal("empty message accepted")
	}

	gate := newGateChat(true)
	solo.SetChatterForTest(gate)
	if _, err := solo.Send(id, "alice", "第一轮", nil); err != nil {
		t.Fatal(err)
	}
	<-gate.start
	refs := []ReferenceInput{
		{Kind: "solo", Target: "older"},
		{Kind: "github_issue", Target: "https://github.com/acme/web/pull/7"},
		{Kind: "github_pr", Target: "acme/web#7"},
		{Kind: "github_issue", Target: "acme/secret#1"},
		{Kind: "github_issue", Target: "acme/down#1"},
	}
	if _, err := solo.SendWithReferences(id, "alice", "参考 @登录重构 和 @#7", nil, refs); err != nil {
		t.Fatal(err)
	}
	if _, err := solo.SendWithReferences(id, "alice", "", nil, []ReferenceInput{{Kind: "solo", Target: "older"}}); err != nil {
		t.Fatal(err)
	}
	// Written after the send but before the queued turn begins: the pin includes it.
	seedSoloMessages(t, db, "older", time.Now(), models.SoloMessage{ID: "x2", Role: "user", Content: "改成直角"})
	close(gate.goOn)

	var msgs []models.SoloMessage
	waitFor(t, "queued turns", func() bool {
		gate.mu.Lock()
		defer gate.mu.Unlock()
		return len(gate.prompts) == 3
	})
	waitFor(t, "user messages", func() bool {
		msgs, _ = solo.Messages(id, "alice")
		n := 0
		for _, m := range msgs {
			if m.Role == "user" && len(m.References) > 0 {
				n++
			}
		}
		return n == 2
	})
	var withRefs models.SoloMessage
	for _, m := range msgs {
		if m.Content == "参考 @登录重构 和 @#7" {
			withRefs = m
		}
	}
	got := withRefs.References
	if len(got) != 4 {
		t.Fatalf("deduped references: %+v", got)
	}
	if got[0].Kind != "solo" || got[0].Anchor != "x2" || got[0].Messages != 2 || got[0].Title != "登录重构" || !strings.HasPrefix(got[0].ID, "ref-") {
		t.Fatalf("solo pin: %+v", got[0])
	}
	if got[1].Kind != models.ReferenceKindGitHubPR || got[1].State != "merged" || got[1].Access != models.ReferenceAccessOK || got[1].Title != "Fix login" {
		t.Fatalf("github ok: %+v", got[1])
	}
	if got[2].Access != models.ReferenceAccessDenied || got[2].Error == "" {
		t.Fatalf("github denied: %+v", got[2])
	}
	if got[3].Access != models.ReferenceAccessUnchecked || got[3].Error != "" {
		t.Fatalf("github unchecked: %+v", got[3])
	}

	gate.mu.Lock()
	prompt, bare := gate.prompts[1], gate.prompts[2]
	gate.mu.Unlock()
	for _, want := range []string{"[用户引用的上下文", "会话「登录重构」(2 条消息", "保留圆角", "改成直角", `read_reference(ref="` + got[0].ID, "gh pr view 7 -R acme/web", "访问失败", "[/用户引用的上下文]", "参考 @登录重构 和 @#7"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if !strings.Contains(prompt, "gh issue view 1 -R acme/down") {
		t.Fatal("unchecked GitHub reference must still be listed")
	}
	if !strings.Contains(bare, "（见引用）") {
		t.Fatalf("reference-only prompt: %s", bare)
	}

	// A new CLI sees earlier references in its preamble.
	preamble := solo.contextPreamble(id, "none", "")
	if !strings.Contains(preamble, "此前用户 @ 引用过的上下文") || !strings.Contains(preamble, got[0].ID) || !strings.Contains(preamble, "GitHub acme/web#7") {
		t.Fatalf("preamble: %s", preamble)
	}
}

func TestPinReferencesMarksMissingTargets(t *testing.T) {
	solo, db, _, id := referenceFixture(t)
	row, _ := solo.Require(id, "alice")
	if solo.pinReferences(row, nil) != nil {
		t.Fatal("no references")
	}
	if err := db.Create(&models.SoloSession{ID: "fresh", UserID: "alice", Title: "新会话"}).Error; err != nil {
		t.Fatal(err)
	}
	out := solo.pinReferences(row, []models.MessageReference{
		{ID: "ref-a", Kind: "solo", Target: "gone", Title: "旧标题"},
		{ID: "ref-b", Kind: "solo", Target: "fresh"},
		{ID: "ref-c", Kind: models.ReferenceKindGitHubIssue, Target: "acme/web#3"},
	})
	if out[0].Error != models.ReferenceErrorDeleted || out[1].Anchor != "" || out[1].Messages != 0 || out[1].Title != "新会话" || out[2].Anchor != "" {
		t.Fatalf("pins: %+v", out)
	}
	block := solo.renderReferenceBlock(append(out, models.MessageReference{ID: "ref-d", Kind: "solo", Target: "x", Title: "坏", Error: "snapshot_unavailable"}))
	for _, want := range []string{"已被删除", "还没有消息", "暂时无法读取", "gh issue view 3"} {
		if !strings.Contains(block, want) {
			t.Fatalf("block missing %q:\n%s", want, block)
		}
	}
	if solo.renderReferenceBlock(nil) != "" {
		t.Fatal("empty block")
	}
	if solo.referenceSummary(models.MessageReference{Target: "x", Anchor: "missing"}) != "" {
		t.Fatal("missing anchor summary")
	}
}

func TestReferenceBlockStaysWithinBudget(t *testing.T) {
	solo, db, _, id := referenceFixture(t)
	row, _ := solo.Require(id, "alice")
	var refs []models.MessageReference
	for i := 0; i < 5; i++ {
		sid := fmt.Sprintf("big%d", i)
		if err := db.Create(&models.SoloSession{ID: sid, UserID: "alice", Title: sid}).Error; err != nil {
			t.Fatal(err)
		}
		seedSoloMessages(t, db, sid, time.Now(), models.SoloMessage{ID: sid + "m", Role: "user", Content: strings.Repeat("长", 5000)})
		refs = append(refs, models.MessageReference{ID: "ref-" + sid, Kind: "solo", Target: sid})
	}
	block := solo.renderReferenceBlock(solo.pinReferences(row, refs))
	if n := len([]rune(block)); n > referenceBlockBudget+500 {
		t.Fatalf("block too long: %d", n)
	}
	if !strings.Contains(block, `read_reference(ref="ref-big0") 的 messages 分页回查`) || strings.Contains(block, "get_history") || !strings.Contains(block, "引用块已达长度上限") {
		t.Fatalf("truncation markers missing:\n%s", block)
	}
}

func TestSearchReferences(t *testing.T) {
	solo, db, gh, id := referenceFixture(t)
	ctx := context.Background()
	now := time.Now()
	for i := 0; i < 22; i++ {
		at := now.Add(-time.Duration(i) * time.Minute)
		if err := db.Create(&models.SoloSession{ID: fmt.Sprintf("s%02d", i), UserID: "alice", Title: fmt.Sprintf("登录 %d", i), Status: models.SoloStatusClosed, LastActiveAt: &at}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.SoloSession{ID: "bob1", UserID: "bob", Title: "登录 bob", Status: models.SoloStatusClosed}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{ID: "pct", UserID: "alice", Title: "100% done", Status: models.SoloStatusClosed}).Error; err != nil {
		t.Fatal(err)
	}

	res, err := solo.SearchReferences(ctx, id, "alice", ReferenceSearchQuery{Lane: "solo", Query: "登录"})
	items := res.Items.([]SoloReferenceItem)
	if err != nil || len(items) != 20 || res.NextPage != 2 || items[0].Target != "s00" || items[0].Kind != "solo" {
		t.Fatalf("page 1: %d next=%d %v", len(items), res.NextPage, err)
	}
	res, _ = solo.SearchReferences(ctx, id, "alice", ReferenceSearchQuery{Lane: "solo", Query: "登录", Page: 2})
	if items := res.Items.([]SoloReferenceItem); len(items) != 2 || res.NextPage != 0 {
		t.Fatalf("page 2: %+v", items)
	}
	res, _ = solo.SearchReferences(ctx, id, "alice", ReferenceSearchQuery{Lane: "solo", Query: "%"})
	if items := res.Items.([]SoloReferenceItem); len(items) != 1 || items[0].Target != "pct" {
		t.Fatalf("LIKE escaping: %+v", items)
	}
	res, _ = solo.SearchReferences(ctx, id, "alice", ReferenceSearchQuery{Lane: "solo"})
	for _, it := range res.Items.([]SoloReferenceItem) {
		if it.Target == id || it.Target == "bob1" {
			t.Fatalf("leaked %s", it.Target)
		}
	}
	if _, err := solo.SearchReferences(ctx, id, "bob", ReferenceSearchQuery{Lane: "solo"}); err == nil {
		t.Fatal("foreign session searchable")
	}
	if _, err := solo.SearchReferences(ctx, id, "alice", ReferenceSearchQuery{Lane: "jira"}); err == nil {
		t.Fatal("unknown lane")
	}

	res, err = solo.SearchReferences(ctx, id, "alice", ReferenceSearchQuery{Lane: "github", Query: "bug", Kinds: []string{"github_issue"}})
	if err != nil || res.Error != nil || res.Source != "search" || len(res.Items.([]githubItem)) != 1 {
		t.Fatalf("github search: %+v %v", res, err)
	}
	if q := gh.queries[len(gh.queries)-1]; !strings.HasPrefix(q, "repo:acme/web ") {
		t.Fatalf("scoped to session repo: %q", q)
	}
	res, _ = solo.SearchReferences(ctx, id, "alice", ReferenceSearchQuery{Lane: "github", Query: "repo:x/y"})
	if res.Error == nil || res.Error.Code != githubErrInvalid || len(res.Items.([]githubItem)) != 0 {
		t.Fatalf("lane error: %+v", res)
	}
	gh.mu.Lock()
	gh.handler = func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
		return true
	}
	gh.mu.Unlock()
	res, _ = solo.SearchReferences(ctx, id, "alice", ReferenceSearchQuery{Lane: "github", Query: "acme/web#55"})
	if res.Error == nil || res.Error.Code != githubErrRateLimited || res.Error.RetryAfterMs != 3000 {
		t.Fatalf("rate limited: %+v", res.Error)
	}
}

func TestSoloCreateCarriesReferencesIntoTheFirstTurn(t *testing.T) {
	solo, db, _, first := referenceFixture(t)
	solo.SetModelAlignerForTest(&recordAligner{})
	gate := newGateChat(false)
	solo.SetChatterForTest(gate)
	if err := db.Create(&models.SoloSession{ID: "older", UserID: "alice", Title: "登录重构", Status: models.SoloStatusClosed}).Error; err != nil {
		t.Fatal(err)
	}
	seedSoloMessages(t, db, "older", time.Now().Add(-time.Hour), models.SoloMessage{ID: "y1", Role: "user", Content: "要求：保留圆角"})
	var before int64
	db.Model(&models.SoloSession{}).Count(&before)
	if _, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA", Text: "x", References: []ReferenceInput{{Kind: "solo", Target: "bobs-or-missing"}}}); err == nil {
		t.Fatal("bad reference accepted at create")
	}
	var after int64
	db.Model(&models.SoloSession{}).Count(&after)
	if after != before {
		t.Fatal("a rejected create left a session behind")
	}
	// Closing the fixture session frees the sandbox slot for the new one.
	if _, err := solo.Close(context.Background(), first, "alice"); err != nil {
		t.Fatal(err)
	}
	view, err := solo.Create(context.Background(), "alice", SoloCreateInput{AgentName: "agentA", References: []ReferenceInput{{Kind: "solo", Target: "older"}, {Kind: "github_pr", Target: "acme/web#7"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = solo.Close(context.Background(), view.ID, "alice") })
	if view.Title != "@登录重构" {
		t.Fatalf("title from first reference: %q", view.Title)
	}
	var msgs []models.SoloMessage
	waitFor(t, "first turn", func() bool {
		msgs, _ = solo.Messages(view.ID, "alice")
		return len(msgs) > 0 && len(msgs[0].References) == 2
	})
	if msgs[0].References[0].Anchor != "y1" || msgs[0].References[1].Access != models.ReferenceAccessOK {
		t.Fatalf("pinned refs: %+v", msgs[0].References)
	}
	waitFor(t, "prompt", func() bool {
		gate.mu.Lock()
		defer gate.mu.Unlock()
		return len(gate.prompts) == 1 && strings.Contains(gate.prompts[0], "保留圆角") && strings.Contains(gate.prompts[0], "（见引用）")
	})
}

func TestSearchLaunchReferences(t *testing.T) {
	solo, db, gh, id := referenceFixture(t)
	ctx := context.Background()
	if err := db.Create(&models.SoloSession{ID: "bob1", UserID: "bob", Title: "bob 的", Status: models.SoloStatusClosed}).Error; err != nil {
		t.Fatal(err)
	}
	res, err := solo.SearchLaunchReferences(ctx, "alice", "agentA", nil, ReferenceSearchQuery{Lane: "solo"})
	items := res.Items.([]SoloReferenceItem)
	if err != nil || len(items) != 1 || items[0].Target != id {
		t.Fatalf("launch solo lane lists every own session: %+v %v", items, err)
	}
	res, _ = solo.SearchLaunchReferences(ctx, "alice", "agentA", nil, ReferenceSearchQuery{Lane: "github", Query: "bug"})
	if res.Source != "none" || !strings.Contains(res.Hint, "没有选择 GitHub 仓库") {
		t.Fatalf("no repos picked: %+v", res)
	}
	var p models.Project
	if err := db.Where("name = ?", "web").First(&p).Error; err != nil {
		t.Fatal(err)
	}
	res, err = solo.SearchLaunchReferences(ctx, "alice", "agentA", []string{" " + p.ID, ""}, ReferenceSearchQuery{Lane: "github", Query: "bug"})
	if err != nil || res.Error != nil || res.Source != "search" {
		t.Fatalf("picked repo: %+v %v", res, err)
	}
	if q := gh.queries[len(gh.queries)-1]; !strings.HasPrefix(q, "repo:acme/web ") {
		t.Fatalf("scoped to picked repo: %q", q)
	}
	if _, err := solo.SearchLaunchReferences(ctx, "alice", "agentA", []string{"missing"}, ReferenceSearchQuery{Lane: "github"}); err == nil {
		t.Fatal("unknown project accepted")
	}
	if _, err := solo.SearchLaunchReferences(ctx, "alice", "", nil, ReferenceSearchQuery{Lane: "nope"}); err == nil {
		t.Fatal("unknown lane")
	}
}
