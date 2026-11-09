package runtime

import (
	"context"
	"errors"
	"github.com/cocofhu/grasp/internal/sandbox"
	"sync/atomic"
	"testing"
	"time"
)

func exactReattachRequest(req NodeReq) NodeReq {
	req.AttachOnly, req.ResetToDefaultModel = true, true
	req.ProviderAttempt = 2
	req.Operation = sandbox.OperationIdentity{ID: "g-original", Attempt: 2, Resource: req.ReattachSandbox, SessionID: "kept-session", Workspace: "/root/workspace", Authorization: "current-grant", SandboxID: 1, Generation: 3}
	return req
}

func TestRunAgentAttachOnlyFinishesExactOriginalOperationWithoutRedispatch(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "finished", true: "running"}[running], func(t *testing.T) {
			b := &survivorBridge{}
			b.appendTurn("g-older-visit", "incorrect old result")
			if running {
				b.log = append(b.log, []byte(`{"type":"prompt_begin","opId":"g-original"}`))
				b.running = "g-original"
				b.finishIn = 150 * time.Millisecond
			} else {
				b.appendTurn("g-original", "original result")
			}
			p, mgr, req := setupReattach(t, b)
			req = exactReattachRequest(req)
			var prepares, dispatches atomic.Int32
			req.PrepareProviderAttempt = func(context.Context) (sandbox.OperationIdentity, error) {
				prepares.Add(1)
				return sandbox.OperationIdentity{}, ErrProviderDispatchUnknown
			}
			req.BeforeProviderDispatch = func(context.Context, sandbox.OperationIdentity) error {
				dispatches.Add(1)
				return ErrProviderDispatchUnknown
			}
			p.opts.NativeAuthorization = func(context.Context, NodeReq) (string, error) { return "current-grant", nil }
			if running {
				b.onFinish = func() { markComplete(p, req) }
			} else {
				markComplete(p, req)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			result, err := p.RunAgent(ctx, req)
			if err != nil {
				b.mu.Lock()
				defer b.mu.Unlock()
				t.Fatal(err, "connects", b.connects, "attach", b.attachOnly)
			}
			want := "original result"
			if running {
				want = "finished after restart"
			}
			if result.OutputMd != want {
				t.Fatal("replayed another visit", result.OutputMd)
			}
			if prepares.Load() != 0 || dispatches.Load() != 0 || mgr.createCount() != 0 || b.chatCount() != 0 {
				t.Fatal("attachment dispatched fresh work", prepares.Load(), dispatches.Load(), mgr.createCount(), b.chatCount())
			}
			b.mu.Lock()
			defer b.mu.Unlock()
			if b.attachOnly != 2 {
				t.Fatal("dedicated original-operation protocol was not used", b.attachOnly)
			}
		})
	}
}

func TestRunAgentAttachOnlyNeverFallsBackForMissingOrForeignOperation(t *testing.T) {
	for _, failure := range []string{"missing-operation", "wrong-session", "incomplete-log", "changed-grant", "missing-receipt", "execution-guard"} {
		t.Run(failure, func(t *testing.T) {
			b := &survivorBridge{}
			if failure != "missing-operation" {
				b.appendTurn("g-original", "original")
			}
			p, mgr, req := setupReattach(t, b)
			req = exactReattachRequest(req)
			if failure == "wrong-session" {
				req.Operation.SessionID = "other-session"
			}
			if failure == "incomplete-log" {
				b.log = b.log[:len(b.log)-1]
			}
			if failure == "missing-receipt" {
				req.Operation.ID = ""
			}
			if failure == "execution-guard" {
				p.opts.CheckExecution = func(context.Context, NodeReq) error { return errors.New("current authority revoked") }
			}
			p.opts.NativeAuthorization = func(context.Context, NodeReq) (string, error) {
				if failure == "changed-grant" {
					return "", errors.New("grant revoked")
				}
				return "current-grant", nil
			}
			markComplete(p, req)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			_, err := p.RunAgent(ctx, req)
			if !errors.Is(err, ErrProviderDispatchUnknown) {
				t.Fatal("unverified operation was not retained as unknown", err)
			}
			if mgr.createCount() != 0 || b.chatCount() != 0 {
				t.Fatal("unknown operation replayed", mgr.createCount(), b.chatCount())
			}
		})
	}
}

func TestRunAgentAttachOnlyRechecksOriginalGrantBeforePublishingSuccess(t *testing.T) {
	b := &survivorBridge{}
	b.appendTurn("g-original", "original")
	p, _, req := setupReattach(t, b)
	req = exactReattachRequest(req)
	markComplete(p, req)
	var checks atomic.Int32
	p.opts.CheckExecution = func(_ context.Context, current NodeReq) error {
		if current.Operation.ID != "g-original" || current.Operation.Authorization != "current-grant" {
			t.Fatal("original grant dropped before publication")
		}
		if checks.Add(1) > 1 {
			return errors.New("grant revoked after native completion")
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if _, err := p.RunAgent(ctx, req); !errors.Is(err, ErrProviderDispatchUnknown) {
		t.Fatal("late revoked result published", err)
	}
	if checks.Load() < 2 || b.chatCount() != 0 {
		t.Fatal("final publication guard missing/replayed", checks.Load(), b.chatCount())
	}
}
