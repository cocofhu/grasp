package services

import (
	"context"
	"testing"
)

func TestOpenForPM(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{failRun: true}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()
	// Wire the hook before opening: setters are startup wiring, and the
	// sandboxes' background start goroutines read it (race under -race).
	called := false
	s.SetAgentSandboxDestroyHook(func(threadID, token string) { called = true })
	s.SetAgentSandboxDestroyHook(nil)
	_ = called
	row, reused, err := s.OpenForPM(ctx, "agentA", "thr-pm", "tok", nil)
	if err != nil || reused || row == nil {
		t.Fatalf("OpenForPM: row=%+v reused=%v err=%v", row, reused, err)
	}
	row2, err := s.OpenAgentSandboxFresh(ctx, "agentA", "thr-fresh2", "tok", nil)
	if err != nil || row2 == nil {
		t.Fatalf("OpenAgentSandboxFresh: %v", err)
	}
}
