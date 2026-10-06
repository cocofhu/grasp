package schedulermcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/platformmcp"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupSchedDB(t *testing.T) (*gorm.DB, *services.PmService, models.Project) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:schedmcp_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(models.AllModels()...); err != nil {
		t.Fatal(err)
	}
	ps := services.NewProjectService(db)
	p, err := ps.Create("SchedMCP", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return db, services.NewPmService(db, nil), p
}

func schedToolCall(id int, name string, args map[string]any) []byte {
	if args == nil {
		args = map[string]any{}
	}
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	return b
}

func schedToolText(t *testing.T, resp []byte) (string, bool) {
	t.Helper()
	var out struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatalf("parse: %v body=%s", err, resp)
	}
	if len(out.Result.Content) == 0 {
		return "", out.Result.IsError
	}
	return out.Result.Content[0].Text, out.Result.IsError
}

func TestSchedulerMCPAuthAndWriteGate(t *testing.T) {
	db, pm, p := setupSchedDB(t)
	h := NewHost(db, pm)
	tok := platformmcp.NewToken()
	h.Restore(tok, p.ID, "agent-a", "thr", "u", false)
	st, resp := h.ServeRPC("agent-b", tok, schedToolCall(1, "list_jobs", nil))
	if st != 401 {
		t.Fatalf("wrong agent want 401 got %d %s", st, resp)
	}
	st, resp = h.ServeRPC("agent-a", tok, schedToolCall(2, "create_job", map[string]any{
		"name": "n", "prompt": "p", "scheduleKind": "every", "scheduleExpr": "1h",
	}))
	if st != 200 {
		t.Fatalf("status=%d", st)
	}
	text, isErr := schedToolText(t, resp)
	if !isErr || !strings.Contains(text, "当前渠道未允许管理定时任务") {
		t.Fatalf("write gate: %s", text)
	}
}

func TestSchedulerMCPCRUDAndCrossAgent(t *testing.T) {
	db, pm, p := setupSchedDB(t)
	h := NewHost(db, pm)
	tokA := platformmcp.NewToken()
	h.Restore(tokA, p.ID, "agent-a", "thr-a", "alice", true)
	tokB := platformmcp.NewToken()
	h.Restore(tokB, p.ID, "agent-b", "thr-b", "bob", true)
	st, resp := h.ServeRPC("agent-a", tokA, schedToolCall(1, "create_job", map[string]any{
		"name": "daily", "prompt": "做日报",
		"scheduleKind": "cron", "scheduleExpr": "0 9 * * *",
	}))
	if st != 200 {
		t.Fatalf("create status=%d", st)
	}
	text, isErr := schedToolText(t, resp)
	if isErr {
		t.Fatalf("create: %s", text)
	}
	var job models.AgentCronJob
	if err := json.Unmarshal([]byte(text), &job); err != nil || job.ID == "" {
		t.Fatalf("job body=%s err=%v", text, err)
	}

	st, resp = h.ServeRPC("agent-a", tokA, schedToolCall(2, "create_job", map[string]any{
		"name": "bad", "prompt": "x", "scheduleKind": "cron", "scheduleExpr": "bad",
	}))
	text, isErr = schedToolText(t, resp)
	if !isErr {
		t.Fatalf("invalid cron should fail, got %s", text)
	}

	st, resp = h.ServeRPC("agent-a", tokA, schedToolCall(3, "create_job", map[string]any{
		"name": "missing",
	}))
	text, isErr = schedToolText(t, resp)
	if !isErr || !strings.Contains(text, "required") {
		t.Fatalf("missing fields: %s", text)
	}

	st, resp = h.ServeRPC("agent-a", tokA, schedToolCall(4, "list_jobs", nil))
	text, isErr = schedToolText(t, resp)
	if isErr || !strings.Contains(text, job.ID) {
		t.Fatalf("list: %s", text)
	}

	st, resp = h.ServeRPC("agent-b", tokB, schedToolCall(5, "delete_job", map[string]any{"jobId": job.ID}))
	text, isErr = schedToolText(t, resp)
	if !isErr || !strings.Contains(text, "not found") {
		t.Fatalf("cross-agent delete: %s", text)
	}
	var n int64
	db.Model(&models.AgentCronJob{}).Where("id = ?", job.ID).Count(&n)
	if n != 1 {
		t.Fatalf("job should remain, n=%d", n)
	}

	threadID := job.ThreadID
	st, resp = h.ServeRPC("agent-a", tokA, schedToolCall(6, "delete_job", map[string]any{"jobId": job.ID}))
	text, isErr = schedToolText(t, resp)
	if isErr {
		t.Fatalf("delete own: %s", text)
	}
	db.Model(&models.AgentCronJob{}).Where("id = ?", job.ID).Count(&n)
	if n != 0 {
		t.Fatalf("deleted n=%d", n)
	}
	var thn int64
	db.Model(&models.ChatThread{}).Where("id = ?", threadID).Count(&thn)
	if thn != 0 {
		t.Fatalf("cron thread should be deleted, n=%d", thn)
	}
}

func TestSchedulerMCPUpdateRunNowAndRuns(t *testing.T) {
	db, pm, p := setupSchedDB(t)
	h := NewHost(db, pm)
	tok := platformmcp.NewToken()
	h.Restore(tok, p.ID, "agent-a", "thr-a", "alice", true)
	_, resp := h.ServeRPC("agent-a", tok, schedToolCall(1, "create_job", map[string]any{
		"name": "hourly", "prompt": "巡检",
		"scheduleKind": "every", "scheduleExpr": "1h",
	}))
	text, isErr := schedToolText(t, resp)
	if isErr {
		t.Fatalf("create: %s", text)
	}
	var job models.AgentCronJob
	if err := json.Unmarshal([]byte(text), &job); err != nil {
		t.Fatal(err)
	}

	_, resp = h.ServeRPC("agent-a", tok, schedToolCall(2, "update_job", map[string]any{
		"jobId": job.ID, "name": "hourly-v2", "enabled": false,
	}))
	text, isErr = schedToolText(t, resp)
	if isErr {
		t.Fatalf("update: %s", text)
	}
	var updated models.AgentCronJob
	if err := json.Unmarshal([]byte(text), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Name != "hourly-v2" || updated.Enabled {
		t.Fatalf("updated=%+v", updated)
	}

	_, resp = h.ServeRPC("agent-a", tok, schedToolCall(3, "run_job_now", map[string]any{"jobId": job.ID}))
	text, isErr = schedToolText(t, resp)
	if isErr {
		t.Fatalf("run_now: %s", text)
	}
	if !strings.Contains(text, `"queued": true`) && !strings.Contains(text, `"queued":true`) {
		t.Fatalf("want queued: %s", text)
	}
	var got models.AgentCronJob
	if err := db.First(&got, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.NextRunAt == nil || got.ClaimedAt != nil {
		t.Fatalf("after run_now: %+v", got)
	}

	run := models.AgentCronRun{ID: "crun-1", JobID: job.ID, Status: "ok"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	_, resp = h.ServeRPC("agent-a", tok, schedToolCall(4, "list_job_runs", map[string]any{"jobId": job.ID}))
	text, isErr = schedToolText(t, resp)
	if isErr || !strings.Contains(text, "crun-1") {
		t.Fatalf("list_job_runs: %s", text)
	}

	tokB := platformmcp.NewToken()

	h.Restore(tokB, p.ID, "agent-b", "thr-b", "bob", true)
	_, resp = h.ServeRPC("agent-b", tokB, schedToolCall(5, "list_job_runs", map[string]any{"jobId": job.ID}))
	text, isErr = schedToolText(t, resp)
	if !isErr || !strings.Contains(text, "not found") {
		t.Fatalf("cross-agent runs: %s", text)
	}

	_, resp = h.ServeRPC("agent-a", tok, schedToolCall(6, "update_job", map[string]any{
		"jobId": job.ID, "scheduleKind": "cron", "scheduleExpr": "not-a-cron",
	}))
	text, isErr = schedToolText(t, resp)
	if !isErr {
		t.Fatalf("bad schedule update should fail: %s", text)
	}
}
