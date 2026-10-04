package services

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fakeRunDeliverer struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (f *fakeRunDeliverer) DeliverRunNotify(projectID, text string, channelIDs []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = channelIDs
	f.calls = append(f.calls, projectID+"|"+text)
	return f.err
}

func (f *fakeRunDeliverer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func setupRunNotifyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&models.Project{}, &models.WorkflowDef{}, &models.NotifyDeliveryReceipt{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedNotifyProject(t *testing.T, db *gorm.DB, enabled bool, events []string, wfMode string, wfEvents []string) (models.Project, models.WorkflowDef) {
	t.Helper()
	p := models.Project{
		ID:   "proj-n1",
		Name: "Demo",
		NotifyPolicy: models.ProjectNotifyPolicy{
			Enabled: boolPtr(enabled), DefaultEvents: events,
			ChannelIDs: []string{"chn-primary"},
		},
	}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	wf := models.WorkflowDef{
		ID: "wf-n1", ProjectID: p.ID, Name: "自我迭代", Status: "published", Version: 1,
		NotifyPolicy: models.WorkflowNotifyPolicy{Mode: wfMode, Events: wfEvents},
	}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	return p, wf
}

func TestAttemptDeliver_claimAndDedup(t *testing.T) {
	db := setupRunNotifyDB(t)
	seedNotifyProject(t, db, true, []string{"waiting_human", "failed"}, "custom", []string{"waiting_human", "failed"})
	d := &fakeRunDeliverer{}
	svc := NewRunNotifyService(db, d, "https://app.example")

	ev := RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "run-1", WorkflowID: "wf-n1",
		NodeID: "gate", NodeLabel: "门禁", Iteration: 1, Kind: "waiting_human",
	}
	svc.AttemptDeliver(ev)
	svc.AttemptDeliver(ev)
	if d.count() != 1 {
		t.Fatalf("deliver calls=%d want 1", d.count())
	}
	if !svc.HasClaimedForTest("run-1", "gate", 1, "waiting_human") {
		t.Fatal("expected receipt")
	}
	text := d.calls[0]
	if !containsAll(text, "等待人工处理", "https://app.example/runs/run-1", "自我迭代", "Demo") {
		t.Fatalf("unexpected text: %s", text)
	}
}

func TestAttemptDeliver_policyMissNoClaim(t *testing.T) {
	db := setupRunNotifyDB(t)
	seedNotifyProject(t, db, true, []string{"failed"}, "inherit", nil)
	d := &fakeRunDeliverer{}
	svc := NewRunNotifyService(db, d, "")
	svc.AttemptDeliver(RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "run-2", WorkflowID: "wf-n1",
		NodeID: "gate", Iteration: 1, Kind: "waiting_human",
	})
	if d.count() != 0 {
		t.Fatal("should not deliver")
	}
	if svc.HasClaimedForTest("run-2", "gate", 1, "waiting_human") {
		t.Fatal("policy miss must not claim")
	}
}

func TestAttemptDeliver_hardClose(t *testing.T) {
	db := setupRunNotifyDB(t)
	seedNotifyProject(t, db, false, []string{"waiting_human", "failed"}, "custom", []string{"waiting_human", "failed"})
	d := &fakeRunDeliverer{}
	svc := NewRunNotifyService(db, d, "")
	svc.AttemptDeliver(RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "run-3", WorkflowID: "wf-n1",
		NodeID: "n", Iteration: 1, Kind: "failed",
	})
	if d.count() != 0 || svc.HasClaimedForTest("run-3", "n", 1, "failed") {
		t.Fatal("hard close should skip")
	}
}

func TestAttemptDeliver_noTargetStillClaims(t *testing.T) {
	db := setupRunNotifyDB(t)
	seedNotifyProject(t, db, true, []string{"failed"}, "custom", []string{"failed"})
	d := &fakeRunDeliverer{err: ErrRunNotifyNoTarget}
	svc := NewRunNotifyService(db, d, "")
	svc.AttemptDeliver(RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "run-4", WorkflowID: "wf-n1",
		NodeID: "impl", Iteration: 2, Kind: "failed",
	})
	if d.count() != 1 {
		t.Fatal("deliverer should still be invoked once")
	}
	if !svc.HasClaimedForTest("run-4", "impl", 2, "failed") {
		t.Fatal("no-op must keep claim")
	}
	// Second attempt must not re-send
	svc.AttemptDeliver(RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "run-4", WorkflowID: "wf-n1",
		NodeID: "impl", Iteration: 2, Kind: "failed",
	})
	if d.count() != 1 {
		t.Fatalf("retry after no-op claim: calls=%d", d.count())
	}
}

func TestAttemptDeliver_sendFailNoRetry(t *testing.T) {
	db := setupRunNotifyDB(t)
	seedNotifyProject(t, db, true, []string{"failed"}, "inherit", nil)
	d := &fakeRunDeliverer{err: errors.New("qq down")}
	svc := NewRunNotifyService(db, d, "")
	ev := RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "run-5", WorkflowID: "wf-n1",
		NodeID: "x", Iteration: 1, Kind: "failed",
	}
	svc.AttemptDeliver(ev)
	svc.AttemptDeliver(ev)
	if d.count() != 1 {
		t.Fatalf("calls=%d want 1", d.count())
	}
}

func TestAttemptDeliver_missingNodeContext(t *testing.T) {
	db := setupRunNotifyDB(t)
	seedNotifyProject(t, db, true, []string{"failed"}, "inherit", nil)
	d := &fakeRunDeliverer{}
	svc := NewRunNotifyService(db, d, "")
	svc.AttemptDeliver(RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "run-6", WorkflowID: "wf-n1",
		Kind: "failed", // no node / iteration
	})
	if d.count() != 0 {
		t.Fatal("early fail without node must skip")
	}
}

func TestClaimReceiptUnique(t *testing.T) {
	db := setupRunNotifyDB(t)
	svc := NewRunNotifyService(db, nil, "")
	ok1, err := svc.ClaimReceiptForTest("r", "n", 1, "waiting_human")
	if err != nil || !ok1 {
		t.Fatalf("first claim: ok=%v err=%v", ok1, err)
	}
	ok2, err := svc.ClaimReceiptForTest("r", "n", 1, "waiting_human")
	if err != nil || ok2 {
		t.Fatalf("second claim: ok=%v err=%v", ok2, err)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func TestFormatRunNotifyMessage_defaultWaitingHuman(t *testing.T) {
	got := FormatRunNotifyMessage(RunNotifyEvent{
		ProjectName: "Demo", WorkflowName: "自我迭代", RunID: "run-1",
		NodeID: "gate", NodeLabel: "门禁", Kind: "waiting_human",
	}, "https://app.example")
	want := "【Grasp】等待人工处理\n项目：Demo\n工作流：自我迭代\nRun：run-1\n节点：门禁\n打开：https://app.example/runs/run-1"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatRunNotifyMessage_omitsNodeLineWhenEmpty(t *testing.T) {
	got := FormatRunNotifyMessage(RunNotifyEvent{
		ProjectName: "P", WorkflowName: "W", RunID: "r", Kind: "failed",
	}, "")
	want := "【Grasp】运行失败\n项目：P\n工作流：W\nRun：r\n打开：/runs/r"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderRunNotifyMessage_emptyAndWhitespaceFallback(t *testing.T) {
	ev := RunNotifyEvent{
		ProjectName: "Demo", WorkflowName: "WF", RunID: "run-x",
		NodeID: "n1", NodeLabel: "节点A", Kind: "waiting_human",
	}
	base := "https://app.example"
	want := FormatRunNotifyMessage(ev, base)
	for _, tmpl := range []string{"", "   ", "\n\t"} {
		got := RenderRunNotifyMessage(ev, base, tmpl)
		if got != want {
			t.Fatalf("tmpl=%q got %q want %q", tmpl, got, want)
		}
	}
}

func TestRenderRunNotifyMessage_customSixKeys(t *testing.T) {
	ev := RunNotifyEvent{
		ProjectName: "Demo", WorkflowName: "WF", RunID: "run-x",
		NodeID: "n1", NodeLabel: "节点A", Kind: "failed",
	}
	tmpl := "【Grasp】{title}\n{project}/{workflow}\n{run_id} · {node}\n{link}"
	got := RenderRunNotifyMessage(ev, "https://app.example", tmpl)
	want := "【Grasp】运行失败\nDemo/WF\nrun-x · 节点A\nhttps://app.example/runs/run-x"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderRunNotifyMessage_customEmptyNodeKeepsLine(t *testing.T) {
	ev := RunNotifyEvent{
		ProjectName: "P", WorkflowName: "W", RunID: "r", Kind: "waiting_human",
	}
	tmpl := "节点：{node}\n打开：{link}"
	got := RenderRunNotifyMessage(ev, "", tmpl)
	want := "节点：\n打开：/runs/r"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestReplaceRunNotifyPlaceholders_unknownKept(t *testing.T) {
	got := ReplaceRunNotifyPlaceholders(
		"hi {project} {unknown} {title}",
		"P", "W", "r", "n", "L", "T",
	)
	want := "hi P {unknown} T"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestAttemptDeliver_usesCustomTemplate(t *testing.T) {
	db := setupRunNotifyDB(t)
	p := models.Project{
		ID: "proj-n1", Name: "Demo",
		NotifyPolicy: models.ProjectNotifyPolicy{
			ChannelIDs: []string{"chn-primary"}, Enabled: boolPtr(true),
			DefaultEvents:        []string{"waiting_human", "failed"},
			WaitingHumanTemplate: "WAIT {project} {run_id} {title}",
			FailedTemplate:       "FAIL {workflow} {node}",
		},
	}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	wf := models.WorkflowDef{
		ID: "wf-n1", ProjectID: p.ID, Name: "自我迭代", Status: "published", Version: 1,
		NotifyPolicy: models.WorkflowNotifyPolicy{Mode: "inherit"},
	}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	d := &fakeRunDeliverer{}
	svc := NewRunNotifyService(db, d, "https://app.example")

	svc.AttemptDeliver(RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "run-tpl", WorkflowID: "wf-n1",
		NodeID: "gate", NodeLabel: "门禁", Iteration: 1, Kind: "waiting_human",
	})
	if d.count() != 1 {
		t.Fatalf("calls=%d", d.count())
	}
	if d.calls[0] != "proj-n1|WAIT Demo run-tpl 等待人工处理" {
		t.Fatalf("unexpected waiting text: %s", d.calls[0])
	}

	svc.AttemptDeliver(RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "run-tpl2", WorkflowID: "wf-n1",
		NodeID: "impl", NodeLabel: "实现", Iteration: 1, Kind: "failed",
	})
	if d.count() != 2 {
		t.Fatalf("calls=%d", d.count())
	}
	if d.calls[1] != "proj-n1|FAIL 自我迭代 实现" {
		t.Fatalf("unexpected failed text: %s", d.calls[1])
	}
}

func TestAttemptDeliver_kindsIndependent(t *testing.T) {
	db := setupRunNotifyDB(t)
	p := models.Project{
		ID: "proj-n1", Name: "Demo",
		NotifyPolicy: models.ProjectNotifyPolicy{
			ChannelIDs: []string{"chn-primary"}, Enabled: boolPtr(true),
			DefaultEvents:        []string{"waiting_human", "failed"},
			WaitingHumanTemplate: "CUSTOM_WAIT {run_id}",
			// failed empty → default formatter
		},
	}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	wf := models.WorkflowDef{
		ID: "wf-n1", ProjectID: p.ID, Name: "WF", Status: "published", Version: 1,
		NotifyPolicy: models.WorkflowNotifyPolicy{Mode: "inherit"},
	}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	d := &fakeRunDeliverer{}
	svc := NewRunNotifyService(db, d, "")

	svc.AttemptDeliver(RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "r1", WorkflowID: "wf-n1",
		NodeID: "a", NodeLabel: "A", Iteration: 1, Kind: "waiting_human",
	})
	svc.AttemptDeliver(RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "r2", WorkflowID: "wf-n1",
		NodeID: "b", NodeLabel: "B", Iteration: 1, Kind: "failed",
	})
	if d.count() != 2 {
		t.Fatalf("calls=%d", d.count())
	}
	if d.calls[0] != "proj-n1|CUSTOM_WAIT r1" {
		t.Fatalf("waiting should use custom: %s", d.calls[0])
	}
	if !strings.Contains(d.calls[1], "【Grasp】运行失败") || !strings.Contains(d.calls[1], "节点：B") {
		t.Fatalf("failed should use default: %s", d.calls[1])
	}
}

func TestAttemptDeliver_completedOptInAndDedup(t *testing.T) {
	db := setupRunNotifyDB(t)
	seedNotifyProject(t, db, true, []string{"waiting_human", "failed", "completed"}, "inherit", nil)
	d := &fakeRunDeliverer{}
	svc := NewRunNotifyService(db, d, "https://app.example")

	ev := RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "run-done", WorkflowID: "wf-n1",
		NodeID: "output_end", NodeLabel: "结束", Iteration: 1, Kind: models.NotifyKindCompleted,
	}
	svc.AttemptDeliver(ev)
	svc.AttemptDeliver(ev)
	if d.count() != 1 {
		t.Fatalf("completed deliver calls=%d want 1", d.count())
	}
	text := d.calls[0]
	if !containsAll(text, "运行完成", "https://app.example/runs/run-done?node=output_end&tab=output", "结束") {
		t.Fatalf("unexpected completed text: %s", text)
	}
	if strings.Contains(text, "运行失败") {
		t.Fatalf("completed must not use failed title: %s", text)
	}
}

func TestAttemptDeliver_completedPolicyMiss(t *testing.T) {
	db := setupRunNotifyDB(t)
	seedNotifyProject(t, db, true, []string{"waiting_human", "failed"}, "inherit", nil)
	d := &fakeRunDeliverer{}
	svc := NewRunNotifyService(db, d, "")
	svc.AttemptDeliver(RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "run-off", WorkflowID: "wf-n1",
		NodeID: "output", Iteration: 1, Kind: models.NotifyKindCompleted,
	})
	if d.count() != 0 {
		t.Fatal("completed must not deliver when not opted in")
	}
	if svc.HasClaimedForTest("run-off", "output", 1, models.NotifyKindCompleted) {
		t.Fatal("policy miss must not claim completed")
	}
}

func TestAttemptDeliver_completedSentinelNode(t *testing.T) {
	db := setupRunNotifyDB(t)
	seedNotifyProject(t, db, true, []string{"completed"}, "custom", []string{"completed"})
	d := &fakeRunDeliverer{}
	svc := NewRunNotifyService(db, d, "https://app.example")
	svc.AttemptDeliver(RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "run-empty", WorkflowID: "wf-n1",
		Kind: models.NotifyKindCompleted, // no node — coerce sentinel
	})
	if d.count() != 1 {
		t.Fatalf("calls=%d want 1", d.count())
	}
	text := d.calls[0]
	if !containsAll(text, "运行完成", "节点：输出", "https://app.example/runs/run-empty?tab=output") {
		t.Fatalf("sentinel completed text: %s", text)
	}
	if strings.Contains(text, "node=") {
		t.Fatalf("sentinel deep link must omit node: %s", text)
	}
	if !svc.HasClaimedForTest("run-empty", models.NotifyCompletedSentinelNodeID, 1, models.NotifyKindCompleted) {
		t.Fatal("expected sentinel receipt")
	}
}

func TestAttemptDeliver_completedCustomTemplate(t *testing.T) {
	db := setupRunNotifyDB(t)
	p := models.Project{
		ID: "proj-n1", Name: "Demo",
		NotifyPolicy: models.ProjectNotifyPolicy{
			ChannelIDs: []string{"chn-primary"}, Enabled: boolPtr(true),
			DefaultEvents:     []string{"completed"},
			CompletedTemplate: "DONE {title} {run_id} {node}",
		},
	}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	wf := models.WorkflowDef{
		ID: "wf-n1", ProjectID: p.ID, Name: "WF", Status: "published", Version: 1,
		NotifyPolicy: models.WorkflowNotifyPolicy{Mode: "inherit"},
	}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatal(err)
	}
	d := &fakeRunDeliverer{}
	svc := NewRunNotifyService(db, d, "")
	svc.AttemptDeliver(RunNotifyEvent{
		ProjectID: "proj-n1", RunID: "r-c", WorkflowID: "wf-n1",
		NodeID: "out", NodeLabel: "结束", Iteration: 1, Kind: models.NotifyKindCompleted,
	})
	if d.count() != 1 || d.calls[0] != "proj-n1|DONE 运行完成 r-c 结束" {
		t.Fatalf("unexpected: %v", d.calls)
	}
}

func TestRunNotifyTitle_completed(t *testing.T) {
	if got := RunNotifyTitle(models.NotifyKindCompleted); got != "运行完成" {
		t.Fatalf("got %q", got)
	}
	if got := RunNotifyTitle(models.NotifyKindFailed); got != "运行失败" {
		t.Fatalf("failed title %q", got)
	}
}

func TestFormatRunNotifyMessage_completedDefault(t *testing.T) {
	got := FormatRunNotifyMessage(RunNotifyEvent{
		ProjectName: "Demo", WorkflowName: "自我迭代", RunID: "run-1",
		NodeID: "output_end", NodeLabel: "结束", Kind: models.NotifyKindCompleted,
	}, "https://app.example")
	want := "【Grasp】运行完成\n项目：Demo\n工作流：自我迭代\nRun：run-1\n节点：结束\n打开：https://app.example/runs/run-1?node=output_end&tab=output"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
