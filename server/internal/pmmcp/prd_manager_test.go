package pmmcp

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type prdManagerFixture struct {
	db     *gorm.DB
	pm     *services.PmService
	drafts *services.RequirementDraftService
	h      *Host
	proj   models.Project
	other  models.Project
	token  string
	audits []services.AuditRecord
}

func setupPrdManagerHost(t *testing.T, enabled []string) *prdManagerFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:pmmcp_prd_"+t.Name()+"_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ps := services.NewProjectService(db)
	proj, err := ps.Create("PrdProj", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ps.Create("OtherPrd", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	pm := services.NewPmService(db, nil)
	en := true
	agent := "leader"
	if _, err := pm.UpdateBinding(proj.ID, &en, &agent, enabled, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := pm.UpdateBinding(other.ID, &en, &agent, []string{MCPPrdManager}, nil, nil); err != nil {
		t.Fatal(err)
	}
	drafts := services.NewRequirementDraftService(db)
	h := NewHost(pm, services.NewPmProgress(pm, nil, nil), nil, nil, services.NewArtifactService(db), nil)
	h.SetRequirementDrafts(drafts)
	fx := &prdManagerFixture{db: db, pm: pm, drafts: drafts, h: h, proj: proj, other: other}
	h.SetAuditRecorder(func(rec services.AuditRecord) {
		fx.audits = append(fx.audits, rec)
	})
	tok := platformmcp.NewToken()
	h.Restore(proj.ID, "thr-prd", "alice", "leader", tok)
	fx.token = tok
	return fx
}

func callPrdTool(t *testing.T, h *Host, projectID, token, tool string, args map[string]any) (status int, result map[string]any, isError bool, raw string) {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	})
	st, resp := h.ServeRPC(projectID, MCPPrdManager, token, body)
	raw = string(resp)
	var rpc struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &rpc); err != nil {
		return st, map[string]any{"_raw": raw}, false, raw
	}
	isError = rpc.Result.IsError
	text := ""
	if len(rpc.Result.Content) > 0 {
		text = rpc.Result.Content[0].Text
	}
	_ = json.Unmarshal([]byte(text), &result)
	if result == nil {
		result = map[string]any{"_raw": text}
	}
	return st, result, isError, raw
}

func TestPrdManagerToolsList(t *testing.T) {
	fx := setupPrdManagerHost(t, []string{MCPPrdManager})
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	st, resp := fx.h.ServeRPC(fx.proj.ID, MCPPrdManager, fx.token, body)
	if st != 200 {
		t.Fatalf("status=%d body=%s", st, resp)
	}
	var listResp struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &listResp); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range listResp.Result.Tools {
		name, _ := tool["name"].(string)
		got[name] = true
	}
	for _, want := range []string{"pm_list_requirement_drafts", "pm_get_requirement_draft", "pm_create_requirement_draft"} {
		if !got[want] {
			t.Fatalf("missing tool %s in %#v", want, got)
		}
	}
	if len(listResp.Result.Tools) != 3 {
		t.Fatalf("first-cut should expose exactly 3 tools, got %d", len(listResp.Result.Tools))
	}
	for _, tool := range listResp.Result.Tools {
		name, _ := tool["name"].(string)
		if strings.HasPrefix(name, "pm_get_progress") || strings.Contains(name, "blocker") {
			t.Fatalf("pm-prd-manager must not expose pm-progress tools: %s", name)
		}
	}
}

func TestPrdManagerListFiltersAndProjection(t *testing.T) {
	fx := setupPrdManagerHost(t, []string{MCPPrdManager})
	openA, err := fx.drafts.Create(fx.proj.ID, services.RequirementDraftCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.drafts.UpdateContent(fx.proj.ID, openA.ID, "Alpha open", "BODY-ALPHA"); err != nil {
		t.Fatal(err)
	}
	openB, err := fx.drafts.Create(fx.proj.ID, services.RequirementDraftCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.drafts.UpdateContent(fx.proj.ID, openB.ID, "Beta open", "BODY-BETA"); err != nil {
		t.Fatal(err)
	}
	done, err := fx.drafts.Create(fx.proj.ID, services.RequirementDraftCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.drafts.UpdateContent(fx.proj.ID, done.ID, "Alpha done", "BODY-DONE"); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.drafts.UpdateStatus(fx.proj.ID, done.ID, models.RequirementDraftStatusDone); err != nil {
		t.Fatal(err)
	}
	otherDraft, err := fx.drafts.Create(fx.other.ID, services.RequirementDraftCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.drafts.UpdateContent(fx.other.ID, otherDraft.ID, "Other secret", "LEAK"); err != nil {
		t.Fatal(err)
	}

	st, result, isErr, raw := callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_list_requirement_drafts", map[string]any{})
	if st != 200 || isErr {
		t.Fatalf("list all: status=%d isErr=%v raw=%s", st, isErr, raw)
	}
	items := mustItems(t, result)
	if len(items) != 3 {
		t.Fatalf("want 3 current-project drafts, got %d raw=%s", len(items), raw)
	}
	st, result, isErr, raw = callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_list_requirement_drafts", map[string]any{"status": "all"})
	if st != 200 || isErr {
		t.Fatalf("list status=all: status=%d isErr=%v raw=%s", st, isErr, raw)
	}
	if n := len(mustItems(t, result)); n != 3 {
		t.Fatalf("status=all want 3 got %d raw=%s", n, raw)
	}
	for _, item := range items {
		if _, ok := item["bodyMarkdown"]; ok {
			t.Fatalf("list must not include bodyMarkdown: %#v", item)
		}
		if _, ok := item["createdAt"]; !ok {
			t.Fatalf("list must include createdAt: %#v", item)
		}
		if item["id"] == otherDraft.ID {
			t.Fatalf("list leaked other project draft: %#v", item)
		}
	}

	st, result, isErr, raw = callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_list_requirement_drafts", map[string]any{"status": "open"})
	if st != 200 || isErr {
		t.Fatalf("list open: %s", raw)
	}
	if n := len(mustItems(t, result)); n != 2 {
		t.Fatalf("open want 2 got %d raw=%s", n, raw)
	}

	st, result, isErr, raw = callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_list_requirement_drafts", map[string]any{"status": "done"})
	if st != 200 || isErr {
		t.Fatalf("list done: %s", raw)
	}
	doneItems := mustItems(t, result)
	if len(doneItems) != 1 || doneItems[0]["id"] != done.ID {
		t.Fatalf("done items=%#v", doneItems)
	}

	st, result, isErr, raw = callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_list_requirement_drafts", map[string]any{"query": "Alpha"})
	if st != 200 || isErr {
		t.Fatalf("list query: %s", raw)
	}
	if n := len(mustItems(t, result)); n != 2 {
		t.Fatalf("query Alpha want 2 got %d raw=%s", n, raw)
	}

	st, result, isErr, raw = callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_list_requirement_drafts", map[string]any{"status": "closed"})
	if st != 200 || !isErr {
		t.Fatalf("illegal status should be isError: status=%d isErr=%v raw=%s", st, isErr, raw)
	}
	if !strings.Contains(raw, "invalid status") {
		t.Fatalf("illegal status error=%s", raw)
	}

	st, result, isErr, raw = callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_list_requirement_drafts", map[string]any{"query": "no-such-title"})
	if st != 200 || isErr {
		t.Fatalf("empty filter must not be error: %s", raw)
	}
	if n := len(mustItems(t, result)); n != 0 {
		t.Fatalf("empty list want 0 got %d", n)
	}
}

func TestPrdManagerGetAndIsolation(t *testing.T) {
	fx := setupPrdManagerHost(t, []string{MCPPrdManager})
	row, err := fx.drafts.Create(fx.proj.ID, services.RequirementDraftCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.drafts.UpdateContent(fx.proj.ID, row.ID, "Mine", "FULL-BODY"); err != nil {
		t.Fatal(err)
	}
	foreign, err := fx.drafts.Create(fx.other.ID, services.RequirementDraftCreateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.drafts.UpdateContent(fx.other.ID, foreign.ID, "Foreign", "SECRET-BODY"); err != nil {
		t.Fatal(err)
	}

	st, result, isErr, raw := callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_get_requirement_draft", map[string]any{
		"draftId":   row.ID,
		"projectId": fx.other.ID,
	})
	if st != 200 || isErr {
		t.Fatalf("get own: %s", raw)
	}
	if result["bodyMarkdown"] != "FULL-BODY" || result["title"] != "Mine" {
		t.Fatalf("get payload=%#v", result)
	}

	before := len(fx.audits)
	st, result, isErr, raw = callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_get_requirement_draft", map[string]any{"draftId": "rd-missing"})
	if st != 200 || !isErr {
		t.Fatalf("missing should be isError: %s", raw)
	}
	if !strings.Contains(raw, "requirement draft not found") {
		t.Fatalf("missing err=%s", raw)
	}
	assertMCPCall(t, fx.audits[before:], "pm_get_requirement_draft", true)

	st, result, isErr, raw = callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_get_requirement_draft", map[string]any{"draftId": foreign.ID})
	if st != 200 || !isErr {
		t.Fatalf("cross-project should be isError: %s", raw)
	}
	if strings.Contains(raw, "SECRET-BODY") || strings.Contains(raw, "Foreign") {
		t.Fatalf("must not leak other project draft: %s", raw)
	}
	if !strings.Contains(raw, "requirement draft not found") {
		t.Fatalf("cross-project err=%s", raw)
	}
}

func TestPrdManagerCreateAndRollback(t *testing.T) {
	fx := setupPrdManagerHost(t, []string{MCPPrdManager})

	before := len(fx.audits)
	st, result, isErr, raw := callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_create_requirement_draft", map[string]any{})
	if st != 200 || isErr {
		t.Fatalf("create empty: %s", raw)
	}
	if result["title"] != services.DefaultRequirementDraftTitle || result["status"] != models.RequirementDraftStatusOpen {
		t.Fatalf("default create=%#v", result)
	}
	if result["bodyMarkdown"] != "" {
		t.Fatalf("default body=%#v", result)
	}
	emptyID, _ := result["id"].(string)
	assertMCPCall(t, fx.audits[before:], "pm_create_requirement_draft", false)

	st, result, isErr, raw = callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_create_requirement_draft", map[string]any{
		"title":        "New PRD",
		"bodyMarkdown": "# hello",
	})
	if st != 200 || isErr {
		t.Fatalf("create with content: %s", raw)
	}
	createdID, _ := result["id"].(string)
	st, got, isErr, raw := callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_get_requirement_draft", map[string]any{"draftId": createdID})
	if st != 200 || isErr || got["title"] != "New PRD" || got["bodyMarkdown"] != "# hello" {
		t.Fatalf("roundtrip get=%#v raw=%s", got, raw)
	}

	st, result, isErr, raw = callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_create_requirement_draft", map[string]any{
		"bodyMarkdown": "only-body",
	})
	if st != 200 || isErr {
		t.Fatalf("create body only: %s", raw)
	}
	if result["title"] != services.DefaultRequirementDraftTitle || result["bodyMarkdown"] != "only-body" {
		t.Fatalf("body-only create=%#v", result)
	}

	before = len(fx.audits)
	tooLong := strings.Repeat("x", services.MaxRequirementDraftTitleRunes+1)
	if utf8.RuneCountInString(tooLong) <= services.MaxRequirementDraftTitleRunes {
		t.Fatal("fixture title not over limit")
	}
	st, result, isErr, raw = callPrdTool(t, fx.h, fx.proj.ID, fx.token, "pm_create_requirement_draft", map[string]any{
		"title": tooLong,
	})
	if st != 200 || !isErr {
		t.Fatalf("over-limit should fail: %s", raw)
	}
	assertMCPCall(t, fx.audits[before:], "pm_create_requirement_draft", true)

	listed, err := fx.drafts.List(fx.proj.ID, services.RequirementDraftListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range listed {
		if utf8.RuneCountInString(row.Title) > services.MaxRequirementDraftTitleRunes {
			t.Fatalf("orphan over-limit draft left behind: %#v", row)
		}
		if row.ID != emptyID && row.Title == services.DefaultRequirementDraftTitle && row.BodyMarkdown == "" {
			// the successful no-arg create is allowed; over-limit must not add another unnamed empty draft
		}
	}
	// Exactly the successful creates remain: empty default, titled, body-only.
	if len(listed) != 3 {
		t.Fatalf("want 3 drafts after rollback, got %d", len(listed))
	}
}

func TestPrdManagerDisabledAndUnavailable(t *testing.T) {
	fx := setupPrdManagerHost(t, []string{MCPProgress})
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	st, resp := fx.h.ServeRPC(fx.proj.ID, MCPPrdManager, fx.token, body)
	if st != 404 || !strings.Contains(string(resp), "mcp disabled") {
		t.Fatalf("disabled: %d %s", st, resp)
	}
	before := len(fx.audits)
	st, resp = fx.h.ServeRPC(fx.proj.ID, MCPPrdManager, fx.token, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"pm_list_requirement_drafts","arguments":{}}}`))
	if st != 404 || !strings.Contains(string(resp), "mcp disabled") {
		t.Fatalf("disabled call: %d %s", st, resp)
	}
	if len(fx.audits) != before {
		t.Fatalf("disabled path must not record mcp.call, audits=%d", len(fx.audits)-before)
	}

	fx2 := setupPrdManagerHost(t, []string{MCPPrdManager})
	fx2.h.SetRequirementDrafts(nil)
	st, result, isErr, raw := callPrdTool(t, fx2.h, fx2.proj.ID, fx2.token, "pm_list_requirement_drafts", nil)
	if st != 200 || !isErr || !strings.Contains(raw, "requirement draft service unavailable") {
		t.Fatalf("unavailable: status=%d isErr=%v result=%#v raw=%s", st, isErr, result, raw)
	}
}

func mustItems(t *testing.T, result map[string]any) []map[string]any {
	t.Helper()
	raw, ok := result["items"].([]any)
	if !ok {
		t.Fatalf("items missing: %#v", result)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("item not object: %#v", item)
		}
		out = append(out, m)
	}
	return out
}

func assertMCPCall(t *testing.T, recs []services.AuditRecord, tool string, wantFail bool) {
	t.Helper()
	for _, rec := range recs {
		if rec.Action != models.AuditActionMCPCall || rec.ResourceID != tool {
			continue
		}
		if rec.Summary != "mcp "+MCPPrdManager+"/"+tool {
			t.Fatalf("audit summary=%q", rec.Summary)
		}
		if rec.Actor.Username != "alice" {
			t.Fatalf("audit actor=%q", rec.Actor.Username)
		}
		if wantFail && rec.Outcome != models.AuditOutcomeFail {
			t.Fatalf("want fail audit, got %#v", rec)
		}
		if !wantFail && rec.Outcome != models.AuditOutcomeOK {
			t.Fatalf("want ok audit, got %#v", rec)
		}
		return
	}
	t.Fatalf("missing mcp.call for %s fail=%v recs=%#v", tool, wantFail, recs)
}
