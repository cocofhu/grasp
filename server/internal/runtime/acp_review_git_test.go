package runtime

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func TestReviewGitNeedsWrapUp(t *testing.T) {
	dirty, unpushed := reviewGitNeedsWrapUp(nil)
	if dirty || unpushed {
		t.Fatalf("nil: dirty=%v unpushed=%v", dirty, unpushed)
	}
	dirty, unpushed = reviewGitNeedsWrapUp(&sandbox.Changes{VCS: "none"})
	if dirty || unpushed {
		t.Fatalf("none: dirty=%v unpushed=%v", dirty, unpushed)
	}
	dirty, unpushed = reviewGitNeedsWrapUp(&sandbox.Changes{VCS: "git", Dirty: true, Pushed: true})
	if !dirty || unpushed {
		t.Fatalf("dirty clean-remote: dirty=%v unpushed=%v", dirty, unpushed)
	}
	dirty, unpushed = reviewGitNeedsWrapUp(&sandbox.Changes{VCS: "git", Dirty: false, Pushed: false, Ahead: 2})
	if dirty || !unpushed {
		t.Fatalf("unpushed commits: dirty=%v unpushed=%v", dirty, unpushed)
	}
	ch := &sandbox.Changes{VCS: "multi", Repos: []sandbox.RepoChanges{
		{Name: "app", Changes: sandbox.Changes{Dirty: false, Pushed: true}},
		{Name: "api", Changes: sandbox.Changes{Dirty: true, Unpushed: 1}},
	}}
	dirty, unpushed = reviewGitNeedsWrapUp(ch)
	if !dirty || !unpushed {
		t.Fatalf("multi: dirty=%v unpushed=%v", dirty, unpushed)
	}
}

func TestFormatDirtyFiles(t *testing.T) {
	got := formatDirtyFiles(&sandbox.Changes{
		VCS:   "git",
		Dirty: true,
		ChangedFiles: []sandbox.ChangedFile{
			{Path: "src/a.go", Status: "modified"},
			{Path: "tmp.log", Status: "untracked"},
		},
	})
	if !strings.Contains(got, "modified src/a.go") || !strings.Contains(got, "untracked tmp.log") {
		t.Fatalf("single-repo list: %q", got)
	}
	got = formatDirtyFiles(&sandbox.Changes{
		VCS: "multi",
		Repos: []sandbox.RepoChanges{
			{Name: "app", Path: "/root/workspace/app", Changes: sandbox.Changes{
				Dirty:        true,
				ChangedFiles: []sandbox.ChangedFile{{Path: "x.go", Status: "untracked"}},
			}},
			{Name: "clean", Path: "/root/workspace/clean", Changes: sandbox.Changes{Dirty: false}},
		},
	})
	if !strings.Contains(got, "仓 `app`") || !strings.Contains(got, "untracked x.go") {
		t.Fatalf("multi list: %q", got)
	}
	if strings.Contains(got, "clean") {
		t.Fatalf("clean repo should be omitted: %q", got)
	}
}

func TestPushWorkingBranchesOmitsGitAdd(t *testing.T) {
	var add, push atomic.Bool
	restore := sandbox.SetExecHook(func(_ context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
		body := execHookBody(command, stdin)
		if strings.Contains(body, "git add -A") {
			add.Store(true)
		}
		if strings.Contains(body, "git push -u origin") {
			push.Store(true)
		}
		return []byte(""), nil
	})
	defer restore()

	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{}).(*acpProvider)
	sb := &sandbox.Sandbox{Name: "sb", Host: "127.0.0.1", Port: 1, WorkspaceDir: "/root/workspace"}
	req := NodeReq{Vars: map[string]any{"repos": `[{"name":"app","url":"https://h/app.git"}]`}}
	p.pushWorkingBranches(context.Background(), sb, req)
	if add.Load() {
		t.Fatal("pushWorkingBranches must not git add -A")
	}
	if !push.Load() {
		t.Fatal("pushWorkingBranches should git push")
	}
}

func TestOfferCommitOnConfirmNoSessionOrNonRepoNode(t *testing.T) {
	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{}).(*acpProvider)
	if t0 := p.OfferCommitOnConfirm(context.Background(), NodeReq{RunID: "r", NodeID: "n", NodeType: "agent", Caps: testImplementCaps}); t0.Msg != "" {
		t.Fatalf("no session: %+v", t0)
	}
	p.sessions["r|n"] = &reactSession{sb: &sandbox.Sandbox{Name: "sb"}}
	if t1 := p.OfferCommitOnConfirm(context.Background(), NodeReq{RunID: "r", NodeID: "n", NodeType: "agent", Caps: testCapsWriting(models.SchemaProposals)}); t1.Msg != "" {
		t.Fatalf("proposal: %+v", t1)
	}
}

func TestReconcileOnConfirmWithoutSessionIsNoOp(t *testing.T) {
	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{}).(*acpProvider)
	req := NodeReq{RunID: "r", NodeID: "n", NodeType: "agent", Caps: testCapsWriting(models.SchemaProposals)}
	if turn := p.ReconcileOnConfirm(context.Background(), req); turn.Msg != "" || turn.AgentSummary != "" {
		t.Fatalf("no session: %+v", turn)
	}
	// A parked session whose ACP died must not chat either.
	p.sessions[reactKey(req)] = &reactSession{sb: &sandbox.Sandbox{Name: "sb"}}
	if turn := p.ReconcileOnConfirm(context.Background(), req); turn.Msg != "" {
		t.Fatalf("disconnected ACP: %+v", turn)
	}
	if got := p.confirmSummaryTurn(context.Background(), req, nil, nil, nil, nil); got != "" {
		t.Fatalf("nil session must yield no summary, got %q", got)
	}
}

func dirtyGitReport() string {
	return strings.Join([]string{
		"VCS\tgit",
		"HEAD\tdeadbeef",
		"BRANCH\tfeat",
		"BASEBRANCH\tmain",
		"BASE\tcafebabe",
		"DIRTY\t1",
		"PUSHED\t0",
		"REMOTESHA\t",
		"UNPUSHED\t0",
		"AHEAD\t0",
		"UNTRACKED\ttmp.log",
	}, "\n")
}

func TestOfferCommitOnConfirmDirtyWithoutACPStillPushes(t *testing.T) {
	var add, push atomic.Bool
	restore := sandbox.SetExecHook(func(_ context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
		body := execHookBody(command, stdin)
		if strings.Contains(body, "printf 'DIRTY") || strings.Contains(body, "UNTRACKED") {
			return []byte(dirtyGitReport()), nil
		}
		if strings.Contains(body, "git add -A") {
			add.Store(true)
		}
		if strings.Contains(body, "git push -u origin") {
			push.Store(true)
		}
		return []byte(""), nil
	})
	defer restore()

	host := mcp.NewHost(newMemStore())
	p := newACPProvider(host, Options{}).(*acpProvider)
	sb := &sandbox.Sandbox{Name: "sb", Host: "127.0.0.1", Port: 1, WorkspaceDir: "/root/workspace"}
	req := NodeReq{RunID: "r", NodeID: "n", NodeType: "agent", Caps: testImplementCaps,
		Vars: map[string]any{"repos": `[{"name":"app","url":"https://h/app.git"}]`}}
	p.sessions[reactKey(req)] = &reactSession{sb: sb}
	turn := p.OfferCommitOnConfirm(context.Background(), req)
	if turn.Msg != "" {
		t.Fatalf("disconnected ACP should not chat, got %q", turn.Msg)
	}
	if add.Load() {
		t.Fatal("confirm wrap-up must not auto git add -A")
	}
	if !push.Load() {
		t.Fatal("confirm wrap-up should still push existing commits")
	}
}

// TestRunAgentReviewConfirmGitWrapUp parks an implement session and, on confirm,
// prompts the agent about leftover dirty files instead of git add -A.
func TestRunAgentReviewConfirmGitWrapUp(t *testing.T) {
	var addAfterPark, wrapPush atomic.Bool
	parked := atomic.Bool{}
	restore := sandbox.SetExecHook(func(_ context.Context, _ string, _ int, command string, stdin io.Reader) ([]byte, error) {
		body := execHookBody(command, stdin)
		if strings.Contains(body, "printf 'DIRTY") || strings.Contains(body, "ls-files --others") {
			return []byte(dirtyGitReport()), nil
		}
		if parked.Load() && strings.Contains(body, "git add -A") {
			addAfterPark.Store(true)
		}
		if parked.Load() && strings.Contains(body, "git push -u origin") {
			wrapPush.Store(true)
		}
		return []byte(""), nil
	})
	defer restore()

	store := newMemStore()
	host := mcp.NewHost(store)
	runID, nodeID := "wrap-run", "impl"
	tok := host.RegisterRun(runID)
	t.Cleanup(func() { host.UnregisterRun(runID) })
	mgr := newFakeManager(t, host, runID, nodeID, tok, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{narration: "implemented", produces: map[string]string{
					mcp.ImplementationResultArtifactName: `{"summary":"done"}`,
					mcp.NodeOutcomeArtifactName:          `{"status":"success"}`,
				}}
			}
			return turnAction{narration: "committed src/a.go, skipped tmp.log"}
		}
	})
	p, _ := newTestProvider(t, host, testOpts(), mgr)
	req := reqWithProfile(NodeReq{
		RunID: runID, NodeID: nodeID, NodeType: "agent", Caps: testImplementCaps, Token: tok,
		KeepAliveForReview: true,
		Config:             map[string]any{"prompt": "build"},
		Vars:               map[string]any{"repos": `[{"name":"app","url":"https://h/app.git"}]`},
	})
	if _, err := p.RunAgent(context.Background(), req); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if !p.HasLiveSession(runID, nodeID) {
		t.Fatal("expected parked session")
	}
	parked.Store(true)
	turn := p.OfferCommitOnConfirm(context.Background(), req)
	if !strings.Contains(turn.Msg, "skipped tmp.log") {
		t.Fatalf("wrap-up narration=%q", turn.Msg)
	}
	var sawPrompt bool
	for i := 0; ; i++ {
		pr := mgr.bridge(0).promptAt(i)
		if pr == "" {
			break
		}
		if strings.Contains(pr, "流转收尾") && strings.Contains(pr, "tmp.log") && strings.Contains(pr, "git add -A") {
			sawPrompt = true
		}
	}
	if !sawPrompt {
		t.Fatal("expected confirm wrap-up prompt listing dirty files and forbidding git add -A")
	}
	if addAfterPark.Load() {
		t.Fatal("wrap-up must not git add -A after parking")
	}
	if !wrapPush.Load() {
		t.Fatal("wrap-up should push after the agent decides")
	}
}
