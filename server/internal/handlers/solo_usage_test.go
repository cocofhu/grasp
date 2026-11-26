package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestListSoloSessionsReportsWorkspaceWideUsage(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, nil)
	for _, row := range []models.Sandbox{
		{Name: "own-usage", Purpose: services.SandboxPurposeSolo, Status: "running", ThreadID: "own-usage"},
		{Name: "other-usage", Purpose: services.SandboxPurposeSolo, Status: "running", ThreadID: "other-usage"},
	} {
		if err := hn.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []models.SoloSession{
		{ID: "own-usage", UserID: "admin", Status: models.SoloStatusRunning},
		{ID: "other-usage", UserID: "someone-else", Status: models.SoloStatusRunning},
	} {
		if err := hn.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	res := hn.do(http.MethodGet, "/api/solo/sessions?status=active", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("list: %d %s", res.Code, res.Body.String())
	}
	var body struct {
		Sessions      []json.RawMessage `json:"sessions"`
		MaxSandboxes  int               `json:"maxSandboxes"`
		UsedSandboxes int               `json:"usedSandboxes"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Sessions) != 1 || body.UsedSandboxes != 2 || body.MaxSandboxes <= 0 {
		t.Fatalf("usage must count every owner while listing only the caller: %+v", body)
	}
}

func TestSoloTaskInfoReportsOwnedUsageAndLinkedWorkflowRuns(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, nil)
	for _, row := range []models.SoloSession{
		{ID: "task", UserID: "admin", Status: models.SoloStatusRunning},
		{ID: "empty-task", UserID: "admin", Status: models.SoloStatusStarting},
		{ID: "zero-task", UserID: "admin", Status: models.SoloStatusRunning},
		{ID: "other-task", UserID: "someone-else", Status: models.SoloStatusRunning},
	} {
		if err := hn.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	reports := []models.TokenUsageReport{
		{ID: "first", Source: models.TokenLedgerSourceSolo, ThreadID: "task", Reported: true},
		{ID: "second", Source: models.TokenLedgerSourceSolo, ThreadID: "task", Reported: true, Partial: true},
		{ID: "zero", Source: models.TokenLedgerSourceSolo, ThreadID: "zero-task", Reported: true},
	}
	if err := hn.db.Create(&reports).Error; err != nil {
		t.Fatal(err)
	}
	events := []models.TokenUsageEvent{
		{
			ReportID: "first", Source: models.TokenLedgerSourceSolo, ThreadID: "task",
			SandboxID: 1, ModelKey: "first-model", InputTokens: 500_000, OutputTokens: 250_000,
			CacheReadTokens: 125_000, CacheWriteTokens: 250_000,
		},
		{
			ReportID: "second", Source: models.TokenLedgerSourceSolo, ThreadID: "task",
			SandboxID: 2, ModelKey: "second-model", ModelFilled: true, InputTokens: 500_000,
		},
		{ReportID: "second", Source: models.TokenLedgerSourceSolo, ThreadID: "task", InputTokens: 2_345},
		{ReportID: "second", Source: models.TokenLedgerSourceSolo, ThreadID: "task", ModelKey: "default", InputTokens: 10_000},
		{Source: models.TokenLedgerSourceSolo, ThreadID: "other-task", ModelKey: "first-model", InputTokens: 999_999},
		{Source: models.TokenLedgerSourceStudio, ThreadID: "task", ModelKey: "first-model", InputTokens: 777_777},
	}
	if err := hn.db.Create(&events).Error; err != nil {
		t.Fatal(err)
	}
	runs := []models.Run{
		{ID: "linked-run", WorkflowID: "wf", WorkflowName: "Release checks", Title: "Verify sandbox", Status: "completed"},
		{ID: "legacy-run", WorkflowID: "legacy-wf", Title: "Legacy sandbox run", Status: "completed"},
		{ID: "unlinked-run", WorkflowID: "wf", WorkflowName: "Release checks", Title: "Another task", Status: "running"},
	}
	if err := hn.db.Create(&models.WorkflowDef{ID: "legacy-wf", Name: "Legacy checks"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := hn.db.Create(&runs).Error; err != nil {
		t.Fatal(err)
	}
	refs := []models.RunReference{
		{ID: "ref-1", RunID: "linked-run", UserID: "admin", Kind: models.ReferenceKindSolo, Target: "task"},
		{ID: "ref-2", RunID: "linked-run", UserID: "admin", Kind: models.ReferenceKindSolo, Target: "task"},
		{ID: "ref-3", RunID: "unlinked-run", UserID: "admin", Kind: models.ReferenceKindSolo, Target: "other-task"},
		{ID: "ref-4", RunID: "unlinked-run", UserID: "someone-else", Kind: models.ReferenceKindSolo, Target: "task"},
		{ID: "ref-5", RunID: "legacy-run", UserID: "admin", Kind: models.ReferenceKindSolo, Target: "task"},
	}
	if err := hn.db.Create(&refs).Error; err != nil {
		t.Fatal(err)
	}

	getInfo := func(id string) services.SoloTaskInfo {
		t.Helper()
		res := hn.do(http.MethodGet, "/api/solo/sessions/"+id+"/task-info", nil)
		var info services.SoloTaskInfo
		if err := json.Unmarshal(res.Body.Bytes(), &info); err != nil || res.Code != http.StatusOK {
			t.Fatalf("task info: %d %s (%v)", res.Code, res.Body.String(), err)
		}
		return info
	}
	info := getInfo("task")
	usage := info.Usage
	if usage.Total != 1_637_345 || usage.InputTokens != 1_012_345 || usage.OutputTokens != 250_000 ||
		usage.CacheReadTokens != 125_000 || usage.CacheWriteTokens != 250_000 || !usage.Reported || !usage.Incomplete {
		t.Fatalf("usage = %+v", usage)
	}
	if len(usage.ByModel) != 3 || usage.ByModel["first-model"].Total() != 1_125_000 ||
		usage.ByModel["second-model"].Total() != 500_000 || !usage.ByModel["second-model"].Filled ||
		usage.ByModel[models.TokenUsageModelUnknown].InputTokens != 12_345 {
		t.Fatalf("usage by model = %+v", usage.ByModel)
	}
	if len(info.WorkflowRuns) != 2 || info.WorkflowRuns[0] != (services.SoloTaskRun{ID: "linked-run", WorkflowName: "Release checks"}) ||
		info.WorkflowRuns[1] != (services.SoloTaskRun{ID: "legacy-run", WorkflowName: "Legacy checks"}) {
		t.Fatalf("workflow runs = %+v", info.WorkflowRuns)
	}
	if empty := getInfo("empty-task"); empty.Usage.Reported || empty.Usage.Total != 0 || len(empty.WorkflowRuns) != 0 {
		t.Fatalf("empty task = %+v", empty)
	}
	if zero := getInfo("zero-task").Usage; !zero.Reported || zero.Total != 0 || zero.Incomplete {
		t.Fatalf("reported zero = %+v", zero)
	}
	res := hn.do(http.MethodGet, "/api/solo/sessions/other-task/task-info", nil)
	if res.Code != http.StatusNotFound {
		t.Fatalf("another user's details: %d %s", res.Code, res.Body.String())
	}
}
