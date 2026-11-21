package runtime

import (
	"context"
	"errors"
	"io"
	"os"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestWorkflowRecoveryPreparationKeepsSelectedSettingsWithoutRecloning(t *testing.T) {
	h := newSharedHarness(t, nil)
	req := h.req("n1")
	req.Repos = []models.SessionRepo{{RepoURL: "https://example.com/private.git"}}
	req.ChatRoute = &ChatRoute{Set: true, ModelID: "applied-model", ReasoningEffort: "high", ServiceTier: "fast"}
	spec, err := h.p.PrepareSandboxRecovery(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(spec.ConfigHome) }()
	if spec.Env["GIT_REPOS"] != "" || spec.ConfigHome == "" || spec.Env[EnvInitialChatModel] != "applied-model" || spec.Env[EnvACPBridgeReasoningEffort] != "high" || spec.Env[EnvACPBridgeServiceTier] != "fast" || h.mgr.createCount() != 0 {
		t.Fatal("recovery preparation cloned/created or lost applied settings")
	}
}

type recoveryRegistry struct {
	countingRegistry
	sb                                 *sandbox.Sandbox
	warm                               bool
	failure                            error
	admissionFailure                   error
	resumes, parks, acquired, released int
}

func (r *recoveryRegistry) ResumeRunSandbox(context.Context, string, string) (*sandbox.Sandbox, bool, bool, error) {
	r.resumes++
	return r.sb, true, r.warm, r.failure
}
func (r *recoveryRegistry) AcquireRunSandbox(context.Context, string) (func(), error) {
	r.acquired++
	return func() { r.released++ }, nil
}
func (r *recoveryRegistry) ParkRunSandbox(string)                { r.parks++ }
func (r *recoveryRegistry) BeginRunSandbox(RunSandboxInfo) error { return r.admissionFailure }

func TestReactRestoreKeepsEnvironmentAndOnlyReplaysColdHistory(t *testing.T) {
	for _, warm := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "snapshot"}[warm], func(t *testing.T) {
			h := newSharedHarness(t, nil)
			h.run(t, h.req("n1"))
			saved := h.p.shared.get(h.runID).sb
			registry := &recoveryRegistry{sb: saved, warm: warm}
			h.p.registry = registry
			before := h.mgr.bridge(0).promptCount()
			sess, found, err := h.p.restoreReactEnvironment(context.Background(), h.req("n1"), []models.ReactMessage{{Role: "human", Text: "keep edited files"}})
			if err != nil || !found || sess == nil || sess.sb.Name != saved.Name {
				t.Fatalf("restore=%+v %v %v", sess, found, err)
			}
			defer sess.acp.Close()
			if h.mgr.createCount() != 1 || registry.unregistered != 0 {
				t.Fatal("restore replaced or destroyed retained environment")
			}
			wantPrompts := 1
			if warm {
				wantPrompts = 0
			}
			if delta := h.mgr.bridge(0).promptCount() - before; delta != wantPrompts {
				t.Fatalf("replay prompts=%d want=%d", delta, wantPrompts)
			}
			if registry.acquired != registry.released || registry.parks != 1 {
				t.Fatalf("task ownership=%+v", registry)
			}
		})
	}
}

func TestReactRestoreSkipsReplayWhenTheAgentKeptItsSession(t *testing.T) {
	h := newSharedHarness(t, nil)
	h.run(t, h.req("n1"))
	saved := h.p.shared.get(h.runID).sb
	h.p.registry = &recoveryRegistry{sb: saved}
	bridge := h.mgr.bridge(0)
	bridge.mu.Lock()
	bridge.resumes = true
	bridge.mu.Unlock()
	before := bridge.promptCount()
	sess, found, err := h.p.restoreReactEnvironment(context.Background(), h.req("n1"), []models.ReactMessage{{Role: "human", Text: "keep edited files"}})
	if err != nil || !found || sess == nil {
		t.Fatalf("restore=%+v %v %v", sess, found, err)
	}
	defer sess.acp.Close()
	if delta := bridge.promptCount() - before; delta != 0 {
		t.Fatalf("an agent that resumed its session got %d replay prompts", delta)
	}
}

func TestReactRestoreErrorNeverFallsBackToEmptySandbox(t *testing.T) {
	h := newSharedHarness(t, nil)
	registry := &recoveryRegistry{failure: io.ErrUnexpectedEOF}
	h.p.registry = registry
	if sess := h.p.rehydrateReact(context.Background(), h.req("n1"), nil); sess != nil {
		t.Fatal("failed restore returned a session")
	}
	if h.mgr.createCount() != 0 || registry.unregistered != 0 {
		t.Fatal("failed restore discarded the saved environment")
	}
}

func TestConnectedDialogueChecksRecoveryBeforeHumanContinuation(t *testing.T) {
	h := newSharedHarness(t, nil)
	h.run(t, h.req("n1"))
	saved := h.p.shared.get(h.runID).sb
	registry := &recoveryRegistry{sb: saved, warm: true}
	h.p.registry = registry
	req := h.req("n1")
	sess, _, err := h.p.restoreReactEnvironment(context.Background(), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.acp.Close()
	if !sess.acp.IsConnected() {
		t.Fatal("test requires a still-connected stale dialogue")
	}
	registry.failure = errors.New("snapshot upload is not durable")
	for _, reply := range []func() ReactTurn{
		func() ReactTurn { return h.p.ReactReply(context.Background(), req, nil, "continue", nil, false) },
		func() ReactTurn { return h.p.ReviseInPlace(context.Background(), req, nil, "revise", nil) },
	} {
		before := h.mgr.bridge(0).promptCount()
		result := reply()
		if result.Err == nil || result.Done || h.mgr.bridge(0).promptCount() != before {
			t.Fatalf("human reply bypassed failed recovery: %+v", result)
		}
	}
	if h.mgr.createCount() != 1 || registry.unregistered != 0 {
		t.Fatal("failed continuation replaced saved environment")
	}
}

func TestSharedRecoveryFailurePreservesSavedWorkspace(t *testing.T) {
	h := newSharedHarness(t, nil)
	h.run(t, h.req("n1"))
	saved := h.p.shared.get(h.runID)
	registry := &recoveryRegistry{sb: saved.sb, warm: true, failure: io.ErrUnexpectedEOF}
	h.p.registry = registry
	_, _, _, handled, err := h.p.openSharedSandbox(context.Background(), h.req("n2"))
	if !handled || !errors.Is(err, errSharedRecoveryPending) {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if h.mgr.createCount() != 1 || h.p.shared.get(h.runID) != saved || registry.unregistered != 0 {
		t.Fatal("failed recovery destroyed shared workspace")
	}
	registry.failure = nil
	h.mgr.failNext = true
	_, _, _, handled, err = h.p.openSharedSandbox(context.Background(), h.req("n2"))
	if !handled || !errors.Is(err, errSharedRecoveryPending) || h.mgr.createCount() != 1 {
		t.Fatalf("failed reprime replaced saved environment: %v", err)
	}
}

func TestWorkflowCapacityRejectsBeforeProvisioning(t *testing.T) {
	h := newSharedHarness(t, nil)
	h.p.shared = nil
	registry := &recoveryRegistry{admissionFailure: errors.New("sandbox limit")}
	h.p.registry = registry
	_, _, _, _, err := h.p.openNodeSandbox(context.Background(), h.req("n1"))
	if err == nil || h.mgr.createCount() != 0 {
		t.Fatalf("capacity rejection created workload: %v", err)
	}
}

func TestReactRestoreOpensFreshSessionAfterUnconfirmedHandoff(t *testing.T) {
	var failing atomic.Bool
	h := newSharedHarness(t, func(int) chatFunc {
		return func(int) turnAction {
			if failing.Load() {
				return turnAction{errorText: "upstream reset", failed: true}
			}
			return turnAction{narration: "ok"}
		}
	})
	h.run(t, h.req("n1"))
	h.p.registry = &recoveryRegistry{sb: h.p.shared.get(h.runID).sb}
	bridge := h.mgr.bridge(0)
	history := []models.ReactMessage{{Role: "human", Text: "keep edited files"}}
	key := reactKey(h.req("n1"))

	failing.Store(true)
	if sess, _, _ := h.p.restoreReactEnvironment(context.Background(), h.req("n1"), history); sess != nil {
		sess.acp.Close()
	}
	failing.Store(false)
	if !h.p.handoffUnconfirmed(key) {
		t.Fatal("a failed handoff turn was treated as delivered")
	}
	bridge.mu.Lock()
	bridge.resumes = true
	bridge.mu.Unlock()
	before := bridge.promptCount()
	sess, found, err := h.p.restoreReactEnvironment(context.Background(), h.req("n1"), history)
	if err != nil || !found || sess == nil {
		t.Fatalf("restore=%+v %v %v", sess, found, err)
	}
	defer sess.acp.Close()
	if bridge.restartCount() != 1 || bridge.promptCount()-before != 1 {
		t.Fatalf("restarts=%d replays=%d; want a fresh native session with one handoff", bridge.restartCount(), bridge.promptCount()-before)
	}
	if h.p.handoffUnconfirmed(key) {
		t.Fatal("a delivered handoff stayed unconfirmed")
	}
	h.p.closeSession(key)
	if len(h.p.handoffs) != 0 {
		t.Fatal("closing the node kept its handoff state")
	}
}
