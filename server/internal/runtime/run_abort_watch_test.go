package runtime

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// GraspReview v3 V3C: a run cancelled mid-stream (AbortRun closes the ACP
// link, which reads as a retryable sandbox fault; ctx is not cancelled) must
// not be retried into a fresh sandbox: one sandbox, one prompt.
func TestRunAbortMidStreamDoesNotRetry(t *testing.T) {
	p, _, _, fm, reg, req := setupProvider(t, stallingChat)
	gm := &gatedManager{fakeManager: fm, entered: make(chan struct{}, 1), release: make(chan struct{})}
	p.mgr = gm
	req.Config["produces"] = ""
	close(gm.release)
	errCh := make(chan error, 1)
	go func() { _, err := p.RunAgent(context.Background(), req); errCh <- err }()
	waitFirstPromptOn(t, gm.fakeManager, 0)
	p.AbortRun(req.RunID)
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("aborted run returned no error")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("RunAgent did not return")
	}
	prompts := 0
	for bi := 0; bi < gm.createCount(); bi++ {
		if b := gm.bridge(bi); b != nil {
			for i := 0; b.promptAt(i) != ""; i++ {
				prompts++
			}
		}
	}
	reg.mu.Lock()
	r, u := reg.registered, reg.unregistered
	reg.mu.Unlock()
	if gm.createCount() != 1 || prompts != 1 {
		t.Fatalf("aborted run retried: creates=%d prompts=%d registered=%d unregistered=%d", gm.createCount(), prompts, r, u)
	}
	p.mu.Lock()
	n := len(p.runAborts)
	p.mu.Unlock()
	if n != 0 {
		t.Fatalf("abort watch leaked: %d entries", n)
	}
}

// GraspReview v3 V3D: finish calls AbortRun for every run; with no RunAgent
// in flight nothing is retained, so the abort record does not grow per run.
func TestRunAbortRecordDoesNotGrow(t *testing.T) {
	p, _, _, _, _, _ := setupProvider(t, stallingChat)
	for i := 0; i < 1000; i++ {
		p.AbortRun(fmt.Sprintf("run-%d", i))
	}
	p.mu.Lock()
	n := len(p.runAborts)
	p.mu.Unlock()
	if n != 0 {
		t.Fatalf("abort record grows per finished run: %d entries", n)
	}
	// A watched run still sees an abort after its snapshot, and its record
	// goes away when the last watcher is done.
	snap, done := p.watchRunAborts("w")
	snap2, done2 := p.watchRunAborts("w")
	if p.abortedSince("w", snap) {
		t.Fatal("no abort yet")
	}
	p.AbortRun("other")
	if p.abortedSince("w", snap) {
		t.Fatal("another run's abort must not count")
	}
	p.AbortRun("w")
	if !p.abortedSince("w", snap) || !p.abortedSince("w", snap2) {
		t.Fatal("abort after snapshot not seen")
	}
	done()
	done() // idempotent
	if !p.abortedSince("w", snap2) {
		t.Fatal("record dropped while a watcher remains")
	}
	done2()
	snap3, done3 := p.watchRunAborts("w")
	defer done3()
	if p.abortedSince("w", snap3) {
		t.Fatal("a new watch must not see the old abort")
	}
	p.mu.Lock()
	n = len(p.runAborts)
	p.mu.Unlock()
	if n != 1 {
		t.Fatalf("entries=%d, want only the live watch", n)
	}
}
