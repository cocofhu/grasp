package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

type fakeDeliverer struct {
	calls        int
	cronCalls    int
	project      string
	text         string
	lastDelivery CronDelivery
}

func (f *fakeDeliverer) Deliver(projectID, text string) error {
	f.calls++
	f.project = projectID
	f.text = text
	return nil
}

func (f *fakeDeliverer) DeliverCron(d CronDelivery) error {
	f.cronCalls++
	f.lastDelivery = d
	f.project = d.ProjectID
	f.text = d.Text
	return nil
}

func setupCronDeliver(t *testing.T) (*CronScheduler, *PmService, models.ChatThread, models.ChatMessage, string) {
	t.Helper()
	db := newTestDB(t)
	pm := NewPmService(db, nil)
	p, err := NewProjectService(db).Create("CronProj", "", nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	thread, err := pm.CreateThread(p.ID, "cron-user", "T", "agent", models.ChatThreadKindUser)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	userMsg, err := pm.AppendMessage(thread.ID, "user", "run please", nil, nil, nil)
	if err != nil {
		t.Fatalf("append user: %v", err)
	}
	if _, err := pm.AppendMessage(thread.ID, "assistant", "done: result text", nil, nil, nil); err != nil {
		t.Fatalf("append assistant: %v", err)
	}
	sched := NewCronScheduler(db, pm, nil, nil, CronTokenHooks{})
	return sched, pm, thread, userMsg, p.ID
}

func TestMaybeDeliverPushesWhenEnabled(t *testing.T) {
	sched, _, thread, userMsg, pid := setupCronDeliver(t)
	fd := &fakeDeliverer{}
	sched.SetChannelDeliverer(fd)

	job := &models.AgentCronJob{ID: "j1", ProjectID: pid, ThreadID: thread.ID, Name: "hourly-pr", DeliverToChannel: true}
	sched.maybeDeliver(job, userMsg)

	if fd.cronCalls != 1 {
		t.Fatalf("expected 1 DeliverCron, got %d (legacy Deliver=%d)", fd.cronCalls, fd.calls)
	}
	if fd.project != pid {
		t.Errorf("delivered to project %q want %q", fd.project, pid)
	}
	if fd.lastDelivery.Text != "done: result text" {
		t.Errorf("delivered text = %q", fd.lastDelivery.Text)
	}
	if fd.lastDelivery.Kind != "changed" {
		t.Errorf("kind = %q want changed", fd.lastDelivery.Kind)
	}
	if fd.lastDelivery.Category != "hourly-pr" {
		t.Errorf("category = %q want hourly-pr", fd.lastDelivery.Category)
	}
	if fd.calls != 0 {
		t.Fatalf("legacy Deliver must not be used by maybeDeliver, got %d", fd.calls)
	}
}

func TestMaybeDeliverClassifiesUnchanged(t *testing.T) {
	sched, pm, thread, userMsg, pid := setupCronDeliver(t)
	if _, err := pm.AppendMessage(thread.ID, "assistant", "PR：无变化", nil, nil, nil); err != nil {
		t.Fatalf("append: %v", err)
	}
	fd := &fakeDeliverer{}
	sched.SetChannelDeliverer(fd)
	job := &models.AgentCronJob{ID: "j1", ProjectID: pid, ThreadID: thread.ID, Name: "每小时PR", DeliverToChannel: true}
	sched.maybeDeliver(job, userMsg)
	if fd.cronCalls != 1 || fd.lastDelivery.Kind != "unchanged" {
		t.Fatalf("got cronCalls=%d kind=%q", fd.cronCalls, fd.lastDelivery.Kind)
	}
}

func TestMaybeDeliverClassifiesFailed(t *testing.T) {
	sched, pm, thread, userMsg, pid := setupCronDeliver(t)
	if _, err := pm.AppendMessage(thread.ID, "assistant", "检查失败：无权访问", nil, nil, nil); err != nil {
		t.Fatalf("append: %v", err)
	}
	fd := &fakeDeliverer{}
	sched.SetChannelDeliverer(fd)
	job := &models.AgentCronJob{ID: "j1", ProjectID: pid, ThreadID: thread.ID, Name: "日报", DeliverToChannel: true}
	sched.maybeDeliver(job, userMsg)
	if fd.cronCalls != 1 || fd.lastDelivery.Kind != "failed" {
		t.Fatalf("got cronCalls=%d kind=%q", fd.cronCalls, fd.lastDelivery.Kind)
	}
}

func TestMaybeDeliverSkipsWhenDisabled(t *testing.T) {
	sched, _, thread, userMsg, pid := setupCronDeliver(t)
	fd := &fakeDeliverer{}
	sched.SetChannelDeliverer(fd)

	job := &models.AgentCronJob{ID: "j1", ProjectID: pid, ThreadID: thread.ID, DeliverToChannel: false}
	sched.maybeDeliver(job, userMsg)

	if fd.cronCalls != 0 || fd.calls != 0 {
		t.Fatalf("expected no delivery when DeliverToChannel=false, got cron=%d legacy=%d", fd.cronCalls, fd.calls)
	}
}

func TestMaybeDeliverNoDelivererIsNoop(t *testing.T) {
	sched, _, thread, userMsg, pid := setupCronDeliver(t)
	job := &models.AgentCronJob{ID: "j1", ProjectID: pid, ThreadID: thread.ID, DeliverToChannel: true}
	// No deliverer set — must not panic.
	sched.maybeDeliver(job, userMsg)
}

func TestDeliverCronFailureOnTimeout(t *testing.T) {
	// review v3: turn timeout / start failure must push Kind=failed via DeliverCron.
	sched, _, thread, _, pid := setupCronDeliver(t)
	fd := &fakeDeliverer{}
	sched.SetChannelDeliverer(fd)

	job := &models.AgentCronJob{
		ID: "j-timeout", ProjectID: pid, ThreadID: thread.ID,
		Name: "每小时PR", DeliverToChannel: true,
	}
	sched.deliverCronFailure(job, "回合超时")
	if fd.cronCalls != 1 {
		t.Fatalf("expected 1 DeliverCron on timeout, got %d", fd.cronCalls)
	}
	if fd.lastDelivery.Kind != "failed" {
		t.Errorf("kind = %q want failed", fd.lastDelivery.Kind)
	}
	if fd.lastDelivery.Text != "回合超时" {
		t.Errorf("text = %q", fd.lastDelivery.Text)
	}
	if fd.lastDelivery.Category != "每小时PR" {
		t.Errorf("category = %q", fd.lastDelivery.Category)
	}
	if fd.calls != 0 {
		t.Fatalf("legacy Deliver must not be used, got %d", fd.calls)
	}

	// Disabled deliverToChannel → silent.
	fd2 := &fakeDeliverer{}
	sched.SetChannelDeliverer(fd2)
	job.DeliverToChannel = false
	sched.deliverCronFailure(job, "回合超时")
	if fd2.cronCalls != 0 {
		t.Fatalf("disabled job must not deliver, got %d", fd2.cronCalls)
	}
}

func TestClassifyCronDeliveryText(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"PR：无变化", "unchanged"},
		{"nothing changed today", "unchanged"},
		{"失败：timeout", "failed"},
		{"error: boom", "failed"},
		{"opened PR #12", "changed"},
		{"", "failed"},
	}
	for _, c := range cases {
		if got := ClassifyCronDeliveryText(c.in); got != c.want {
			t.Errorf("ClassifyCronDeliveryText(%q) = %q want %q", c.in, got, c.want)
		}
	}
}
