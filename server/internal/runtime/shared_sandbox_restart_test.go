package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/sandbox"
)

// gatewayAttachManager wraps fakeManager with the real Manager's attach
// shape: the handle it rebuilds has no bridge password and no ConfigRoot
// (sandboxFromGW never fills them), and Attach would push a runtime bundle.
type gatewayAttachManager struct {
	*fakeManager
	mu       sync.Mutex
	attach   []string
	attachNE []string
}

func (m *gatewayAttachManager) lookup(id string) (*sandbox.Sandbox, error) {
	var idx int
	if _, err := fmt.Sscanf(id, "fake-sb-%d", &idx); err != nil {
		return nil, errors.New("unknown sandbox")
	}
	b := m.bridge(idx)
	if b == nil {
		return nil, errors.New("gone")
	}
	host, port := b.hostPort()
	return &sandbox.Sandbox{ID: id, Name: id, Host: host, Port: port, WorkspaceDir: "/root/workspace"}, nil
}

func (m *gatewayAttachManager) Attach(_ context.Context, id string) (*sandbox.Sandbox, error) {
	m.mu.Lock()
	m.attach = append(m.attach, id)
	m.mu.Unlock()
	return m.lookup(id)
}

func (m *gatewayAttachManager) AttachNoEnsure(_ context.Context, id string) (*sandbox.Sandbox, error) {
	m.mu.Lock()
	m.attachNE = append(m.attachNE, id)
	m.mu.Unlock()
	return m.lookup(id)
}

type restartOutcome struct {
	created, replacements int
	attach, attachNE      []string
	journal               []string
	staleChat             string
	err                   error
}

// runSharedRestart has an "old process" open node-a in the run's shared
// container and die mid-turn (lease never released, staleOp left running on
// the node's chat), then re-enters node-a from a fresh provider and
// coordinator the way the engine does after a restart.
func runSharedRestart(t *testing.T, staleOp string) restartOutcome {
	t.Helper()
	f := newSharedFlow(t, map[string]string{"dev-a": `{"acpBackend":"cursor"}`},
		func(int) chatFunc {
			return func(int) turnAction { return turnAction{narration: "turn", outcome: true} }
		})
	p1 := f.provider(t, BackendCursor)
	req := f.req("node-a", "dev-a")
	req.Config["produces"] = ""
	ls, err := p1.specLayered(req, true, false)
	if err != nil {
		t.Fatal(err)
	}
	sb, _, home, err := p1.openSandbox(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	b0 := f.mgr.bridge(0)
	b0.mu.Lock()
	stale := ""
	for _, ch := range b0.openChats {
		if ch.Title == nodeChatTitle(req) {
			stale = ch.ID
		}
	}
	if staleOp != "" && stale != "" {
		b0.staleRunning = map[string]string{stale: staleOp}
	}
	b0.mu.Unlock()
	f.reg.binding = &RunSandboxInfo{Name: sb.Name, RunID: f.runID, NodeID: "node-a", HomeDir: home,
		Scope: sandboxScopeRun, Generation: 1, EnvFingerprint: ls.fingerprint, Token: f.token}

	gm := &gatewayAttachManager{fakeManager: f.mgr}
	f.coord = newSharedSandboxCoordinator()
	p2 := f.provider(t, BackendCursor)
	p2.mgr = gm
	req.ReattachSandbox = sb.Name
	_, runErr := p2.RunAgent(context.Background(), req)

	f.reg.mu.Lock()
	repl := f.reg.replacements
	f.reg.mu.Unlock()
	gm.mu.Lock()
	defer gm.mu.Unlock()
	return restartOutcome{created: f.mgr.createCount(), replacements: repl, attach: gm.attach, attachNE: gm.attachNE,
		journal: b0.journalCopy(), staleChat: stale, err: runErr}
}

// After a restart the node re-enters the same shared container: it is
// re-attached without a runtime bundle push, with the bridge password from
// the platform record, instead of being replaced by a generation-2 container
// (which loses the workspace).
func TestSharedRestartReattachKeepsSharedContainer(t *testing.T) {
	out := runSharedRestart(t, "")
	if out.err != nil {
		t.Fatalf("RunAgent: %v", out.err)
	}
	if out.created != 1 || out.replacements != 0 {
		t.Fatalf("restart replaced the shared container: created=%d replacements=%d", out.created, out.replacements)
	}
	if len(out.attach) != 0 || len(out.attachNE) != 1 {
		t.Fatalf("re-attach must use AttachNoEnsure only: Attach=%v AttachNoEnsure=%v", out.attach, out.attachNE)
	}
}

// The turn the previous process left running on the node's chat is
// cancelled (op read from queue_state) and its chat deleted before the
// node's prompt is sent again, so two turns never run in one workspace.
func TestSharedRestartReentryStopsStaleTurnBeforeResend(t *testing.T) {
	out := runSharedRestart(t, "op-before-restart")
	if out.err != nil {
		t.Fatalf("RunAgent: %v", out.err)
	}
	if out.staleChat == "" {
		t.Fatal("old process did not open a node chat")
	}
	firstPrompt, cancelAt, deleteAt := -1, -1, -1
	for i, e := range out.journal {
		switch {
		case e == "cancel:op-before-restart" && cancelAt < 0:
			cancelAt = i
		case e == "delete:"+out.staleChat && deleteAt < 0:
			deleteAt = i
		case strings.HasPrefix(e, "prompt:") && firstPrompt < 0:
			firstPrompt = i
		}
	}
	if cancelAt < 0 || deleteAt < 0 || firstPrompt < 0 || !(cancelAt < deleteAt && deleteAt < firstPrompt) {
		t.Fatalf("stale turn not stopped before resend: journal=%v", out.journal)
	}
}
