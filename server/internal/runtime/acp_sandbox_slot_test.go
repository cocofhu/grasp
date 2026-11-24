package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

type slotRegistry struct {
	countingRegistry
	full  int
	calls int
}

func (r *slotRegistry) BeginRunSandbox(RunSandboxInfo) error {
	r.calls++
	if r.calls <= r.full {
		return fmt.Errorf("limit 1: %w", ErrSandboxCapacity)
	}
	return nil
}

func withSlotPoll(t *testing.T, d time.Duration) {
	t.Helper()
	prev := sandboxSlotPoll
	sandboxSlotPoll = d
	t.Cleanup(func() { sandboxSlotPoll = prev })
}

func TestWaitRunSandboxSlotWaitsForCapacity(t *testing.T) {
	withSlotPoll(t, time.Millisecond)
	reg := &slotRegistry{full: 3}
	var notices []models.AcpEvent
	p := &acpProvider{registry: reg, emit: func(_, _ string, ev []models.AcpEvent, _ bool) { notices = append(notices, ev...) }}
	if err := p.waitRunSandboxSlot(context.Background(), NodeReq{RunID: "r", NodeID: "n"}, "sb", ""); err != nil {
		t.Fatal(err)
	}
	if reg.calls != 4 {
		t.Fatalf("admission attempts = %d, want 4", reg.calls)
	}
	if len(notices) != 1 || notices[0].Title != "等待沙箱名额" {
		t.Fatalf("notices = %+v, want one wait notice", notices)
	}
}

func TestWaitRunSandboxSlotStopsWithContext(t *testing.T) {
	withSlotPoll(t, time.Hour)
	reg := &slotRegistry{full: 1 << 30}
	p := &acpProvider{registry: reg}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := p.waitRunSandboxSlot(ctx, NodeReq{}, "sb", "")
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrSandboxCapacity) || reg.calls != 1 {
		t.Fatalf("err = %v calls = %d, want context.Canceled", err, reg.calls)
	}
}

func TestWaitRunSandboxSlotReportsDeadline(t *testing.T) {
	withSlotPoll(t, time.Hour)
	p := &acpProvider{registry: &slotRegistry{full: 1 << 30}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	err := p.waitRunSandboxSlot(ctx, NodeReq{}, "sb", "")
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrSandboxCapacity) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
}

func TestWaitRunSandboxSlotReportsCancelCause(t *testing.T) {
	withSlotPoll(t, time.Hour)
	p := &acpProvider{registry: &slotRegistry{full: 1 << 30}}
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(&nodeBudgetError{limit: time.Minute, used: time.Minute})
	err := p.waitRunSandboxSlot(ctx, NodeReq{}, "sb", "")
	if !errors.Is(err, ErrNodeBudget) {
		t.Fatalf("err = %v, want ErrNodeBudget", err)
	}
}

func TestWaitRunSandboxSlotReturnsOtherErrors(t *testing.T) {
	boom := errors.New("db down")
	p := &acpProvider{registry: &recoveryRegistry{admissionFailure: boom}}
	if err := p.waitRunSandboxSlot(context.Background(), NodeReq{}, "sb", ""); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
