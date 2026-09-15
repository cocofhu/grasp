package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
)

// The TestRunAgent* / TestReact* cases below are the fake end-to-end suite: they
// drive the real acpProvider through an in-memory fakeBridge + fakeManager
// (no Docker, no Cursor API key) over the full RunAgent / React flow. CI's
// blocking `server:e2e:fake` job selects them by name via
// `go test -run 'TestRunAgent|TestReact'`, as the credential-free counterpart to
// the live `server:e2e` job (TestCursorLive*). Keep new fake-e2e cases under one
// of those two name prefixes so the job keeps picking them up.

// testOpts is a fast, Docker-free provider config: tiny backoff so retries
// don't slow the suite, short idle window for the stall test.
func testOpts() Options {
	return Options{
		SandboxImage:        "fake",
		SandboxMaxAttempts:  3,
		SandboxRetryBackoff: time.Millisecond,
		ChatIdleTimeout:     80 * time.Millisecond,
		ChatTimeout:         5 * time.Second,
	}
}

func setupProvider(t *testing.T, chatFor func(attempt int) chatFunc) (*acpProvider, *mcp.Host, *memStore, *fakeManager, *countingRegistry, NodeReq) {
	return setupProviderBackend(t, BackendCursor, chatFor)
}

func setupProviderBackend(t *testing.T, backend AcpBackend, chatFor func(attempt int) chatFunc) (*acpProvider, *mcp.Host, *memStore, *fakeManager, *countingRegistry, NodeReq) {
	t.Helper()
	store := newMemStore()
	host := mcp.NewHost(store)
	runID, nodeID := "run-1", "node-1"
	tok := host.RegisterRun(runID)
	t.Cleanup(func() { host.UnregisterRun(runID) })
	profiles := writeAgent(t, "test-agent", `{"acpBackend":"`+string(backend)+`","env":{"GRASP_CURSOR_API_KEY":"fake","GRASP_CLAUDE_API_KEY":"fake","GRASP_CODEBUDDY_API_KEY":"fake","GRASP_TRAE_API_KEY":"fake"}}`)
	mgr := newFakeManager(t, host, runID, nodeID, tok, chatFor)
	opts := testOpts()
	opts.ProfilesRoot = profiles
	p, reg := newTestProviderBackend(t, host, opts, mgr, backend)
	req := NodeReq{RunID: runID, NodeID: nodeID, NodeType: "agent", Token: tok,
		Config: map[string]any{"prompt": "do it", "produces": "report.md", "agent_profile": "test-agent"}, Vars: map[string]any{}}
	return p, host, store, mgr, reg, req
}

func TestRunAgentHappyPath(t *testing.T) {
	p, _, store, mgr, reg, req := setupProvider(t, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{narration: "done work", produces: map[string]string{"report.md": "# report\nok"}}
		}
	})
	res, err := p.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res.OutputMd == "" {
		t.Error("expected narration output")
	}
	if c, ok := store.Get("run-1", "report.md"); !ok || c == "" {
		t.Errorf("produces not stored: got %q ok=%v", c, ok)
	}
	if mgr.createCount() != 1 {
		t.Errorf("create count = %d, want 1", mgr.createCount())
	}
	if !reg.balanced() {
		t.Errorf("sandbox registry not balanced: %+v", reg)
	}
}

func TestRunAgentRetryThenSucceed(t *testing.T) {
	p, _, store, mgr, reg, req := setupProvider(t, func(attempt int) chatFunc {
		return func(int) turnAction {
			if attempt < 2 {
				return turnAction{dropConn: true} // ErrConnClosed -> retryable
			}
			return turnAction{narration: "recovered", produces: map[string]string{"report.md": "ok"}}
		}
	})
	res, err := p.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RunAgent should recover after retries: %v", err)
	}
	if res.OutputMd == "" {
		t.Error("expected narration after recovery")
	}
	if _, ok := store.Get("run-1", "report.md"); !ok {
		t.Error("produces missing after recovery")
	}
	if mgr.createCount() != 3 {
		t.Errorf("create count = %d, want 3 (2 failed + 1 ok)", mgr.createCount())
	}
	if !reg.balanced() {
		t.Errorf("sandbox leaked across retries: %+v", reg)
	}
}

func TestRunAgentRetryExhausted(t *testing.T) {
	p, _, _, mgr, reg, req := setupProvider(t, func(int) chatFunc {
		return func(int) turnAction { return turnAction{dropConn: true} }
	})
	p.opts.SandboxMaxAttempts = 2
	_, err := p.RunAgent(context.Background(), req)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !isRetryableSandboxErr(err) {
		t.Errorf("final error should still classify retryable, got %v", err)
	}
	if mgr.createCount() != 2 {
		t.Errorf("create count = %d, want 2 (== SandboxMaxAttempts)", mgr.createCount())
	}
	if !reg.balanced() {
		t.Errorf("sandbox leaked: %+v", reg)
	}
}

func TestRunAgentIdleTimeoutRetries(t *testing.T) {
	p, _, store, mgr, _, req := setupProvider(t, func(attempt int) chatFunc {
		return func(int) turnAction {
			if attempt == 0 {
				return turnAction{stall: true} // no events -> ErrChatIdle -> retryable
			}
			return turnAction{narration: "second try", produces: map[string]string{"report.md": "ok"}}
		}
	})
	p.opts.SandboxMaxAttempts = 2
	res, err := p.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("idle timeout should retry and succeed: %v", err)
	}
	if res.OutputMd == "" {
		t.Error("expected narration on the retry")
	}
	if _, ok := store.Get("run-1", "report.md"); !ok {
		t.Error("produces missing after idle retry")
	}
	if mgr.createCount() != 2 {
		t.Errorf("create count = %d, want 2", mgr.createCount())
	}
}

func TestRunAgentNonRetryableError(t *testing.T) {
	p, _, _, mgr, _, req := setupProvider(t, func(int) chatFunc {
		return func(int) turnAction { return turnAction{sendError: "model refused"} }
	})
	_, err := p.RunAgent(context.Background(), req)
	if err == nil {
		t.Fatal("expected an error from the agent-reported failure")
	}
	if isRetryableSandboxErr(err) {
		t.Errorf("agent error must not be retryable: %v", err)
	}
	if mgr.createCount() != 1 {
		t.Errorf("create count = %d, want 1 (no retry on agent error)", mgr.createCount())
	}
}

// reactChat scripts a react sandbox: turn 0 (open or prime) and later turns.
func reactSetup(t *testing.T, chatFor func(attempt int) chatFunc) (*acpProvider, *mcp.Host, *memStore, *fakeManager, NodeReq) {
	return reactSetupBackend(t, BackendCursor, chatFor)
}

func reactSetupBackend(t *testing.T, backend AcpBackend, chatFor func(attempt int) chatFunc) (*acpProvider, *mcp.Host, *memStore, *fakeManager, NodeReq) {
	store := newMemStore()
	host := mcp.NewHost(store)
	runID, nodeID := "run-r", "node-r"
	tok := host.RegisterRun(runID)
	t.Cleanup(func() { host.UnregisterRun(runID) })
	profiles := writeAgent(t, "react-agent", `{"acpBackend":"`+string(backend)+`","env":{"GRASP_CURSOR_API_KEY":"fake","GRASP_CLAUDE_API_KEY":"fake","GRASP_CODEBUDDY_API_KEY":"fake","GRASP_TRAE_API_KEY":"fake"}}`)
	mgr := newFakeManager(t, host, runID, nodeID, tok, chatFor)
	opts := testOpts()
	opts.ProfilesRoot = profiles
	p, _ := newTestProviderBackend(t, host, opts, mgr, backend)
	req := NodeReq{RunID: runID, NodeID: nodeID, NodeType: "react", Token: tok,
		Config: map[string]any{"prompt": "clarify", "max_rounds": 3, "agent_profile": "react-agent"}, Vars: map[string]any{}}
	return p, host, store, mgr, req
}

func clarified() map[string]string {
	return map[string]string{mcp.ClarifiedRequirementArtifactName: mcp.MinimalValidClarifiedRequirementJSON}
}

func TestReactOpenPausesThenReplyFinishes(t *testing.T) {
	p, _, store, _, req := reactSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 { // opening turn raises a question
				return turnAction{narration: "need info", questions: []models.ReactQuestion{{ID: "q1", Prompt: "?", Options: []models.ReactOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}}}}
			}
			return turnAction{narration: "clarified", produces: clarified()}
		}
	})
	open := p.ReactOpen(context.Background(), req)
	if open.Done {
		t.Fatal("opening turn with a question should pause, not finish")
	}
	if len(open.Questions) == 0 {
		t.Fatal("expected questions on the opening turn")
	}
	hist := []models.ReactMessage{{Role: "agent", Text: "need info"}, {Role: "human", Text: "answer"}}
	reply := p.ReactReply(context.Background(), req, hist, "answer", nil, false)
	if !reply.Done {
		t.Fatalf("reply should finish the clarification, got %+v", reply)
	}
	if _, ok := store.Get("run-r", mcp.ClarifiedRequirementArtifactName); !ok {
		t.Error("clarified requirement not written on finish")
	}
}

func TestReactReplyRehydratesAfterSessionLoss(t *testing.T) {
	p, _, store, mgr, req := reactSetup(t, func(attempt int) chatFunc {
		return func(turn int) turnAction {
			if attempt == 0 { // first sandbox: opening turn asks a question
				return turnAction{narration: "need info", questions: []models.ReactQuestion{{ID: "q1", Prompt: "?", Options: []models.ReactOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}}}}
			}
			// second sandbox: turn 0 is the rehydrate prime, turn 1 is the reply.
			if turn == 0 {
				return turnAction{narration: "context restored"}
			}
			return turnAction{narration: "clarified", produces: clarified()}
		}
	})
	open := p.ReactOpen(context.Background(), req)
	if len(open.Questions) == 0 {
		t.Fatal("expected opening question")
	}
	// Simulate a lost live session: close the ACP so IsConnected() is false.
	key := reactKey(req)
	p.mu.Lock()
	sess := p.sessions[key]
	p.mu.Unlock()
	if sess == nil {
		t.Fatal("expected a live react session after open")
	}
	sess.acp.Close()

	hist := []models.ReactMessage{{Role: "agent", Text: "need info"}, {Role: "human", Text: "answer"}}
	reply := p.ReactReply(context.Background(), req, hist, "answer", nil, false)
	if !reply.Done {
		t.Fatalf("rehydrated reply should finish, got %+v", reply)
	}
	if _, ok := store.Get("run-r", mcp.ClarifiedRequirementArtifactName); !ok {
		t.Error("clarified requirement not written after rehydrate")
	}
	if mgr.createCount() != 2 {
		t.Errorf("create count = %d, want 2 (open + rehydrate)", mgr.createCount())
	}
	// The rebuilt sandbox's first prompt must be the recovery/prime prompt.
	if prime := mgr.bridge(1).promptAt(0); !strings.Contains(prime, "会话恢复") {
		t.Errorf("rehydrate prime prompt missing recovery marker: %q", prime)
	}
}

func TestReactReplyRehydrateFailureNotDone(t *testing.T) {
	p, _, _, mgr, req := reactSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			return turnAction{narration: "need info", questions: []models.ReactQuestion{{ID: "q1", Prompt: "?", Options: []models.ReactOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}}}}
		}
	})
	p.opts.SandboxMaxAttempts = 1
	// Fail the rehydrate sandbox creation (the 2nd Create).
	mgr.createErr = func(attempt int) error {
		if attempt == 1 {
			return errors.New("no capacity")
		}
		return nil
	}
	open := p.ReactOpen(context.Background(), req)
	if len(open.Questions) == 0 {
		t.Fatal("expected opening question")
	}
	key := reactKey(req)
	p.mu.Lock()
	sess := p.sessions[key]
	p.mu.Unlock()
	sess.acp.Close()

	hist := []models.ReactMessage{{Role: "human", Text: "answer"}}
	reply := p.ReactReply(context.Background(), req, hist, "answer", nil, false)
	if reply.Done {
		t.Error("rehydrate failure must NOT silently finish the node (Done should be false)")
	}
	if !strings.Contains(reply.Msg, "重建") {
		t.Errorf("expected a rebuild-failure message, got %q", reply.Msg)
	}
}

func TestRunAgent_ClaudeCode(t *testing.T) {
	p, _, store, _, _, req := setupProviderBackend(t, BackendClaudeCode, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{narration: "claude ok", produces: map[string]string{"report.md": "cc"}}
		}
	})
	res, err := p.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res.OutputMd == "" {
		t.Error("expected narration")
	}
	if _, ok := store.Get("run-1", "report.md"); !ok {
		t.Error("produces missing")
	}
	if p.Name() != string(BackendClaudeCode) {
		t.Errorf("Name = %q", p.Name())
	}
}

func TestRunAgent_CodeBuddy(t *testing.T) {
	p, _, store, _, _, req := setupProviderBackend(t, BackendCodeBuddy, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{narration: "buddy ok", produces: map[string]string{"report.md": "cb"}}
		}
	})
	if _, err := p.RunAgent(context.Background(), req); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if _, ok := store.Get("run-1", "report.md"); !ok {
		t.Error("produces missing")
	}
}

func TestRunAgent_Trae(t *testing.T) {
	p, _, store, _, _, req := setupProviderBackend(t, BackendTrae, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{narration: "trae ok", produces: map[string]string{"report.md": "tr"}}
		}
	})
	if _, err := p.RunAgent(context.Background(), req); err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if _, ok := store.Get("run-1", "report.md"); !ok {
		t.Error("produces missing")
	}
}

func TestReact_ClaudeCode(t *testing.T) {
	testReactBackend(t, BackendClaudeCode)
}

func TestReact_CodeBuddy(t *testing.T) {
	testReactBackend(t, BackendCodeBuddy)
}

func TestReact_Trae(t *testing.T) {
	testReactBackend(t, BackendTrae)
}

func testReactBackend(t *testing.T, backend AcpBackend) {
	t.Helper()
	p, _, store, _, req := reactSetupBackend(t, backend, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{narration: "q", questions: []models.ReactQuestion{{ID: "q1", Prompt: "?", Options: []models.ReactOption{{ID: "a", Label: "A"}}}}}
			}
			return turnAction{narration: "done", produces: clarified()}
		}
	})
	open := p.ReactOpen(context.Background(), req)
	if open.Done {
		t.Fatal("expected pause")
	}
	reply := p.ReactReply(context.Background(), req, []models.ReactMessage{{Role: "human", Text: "a"}}, "a", nil, false)
	if !reply.Done {
		t.Fatalf("expected done, got %+v", reply)
	}
	if _, ok := store.Get("run-r", mcp.ClarifiedRequirementArtifactName); !ok {
		t.Error("artifact missing")
	}
}

func samplePendingQuestion() []models.ReactQuestion {
	return []models.ReactQuestion{{
		ID: "q1", Prompt: "scope?",
		Options: []models.ReactOption{
			{ID: "a", Label: "A", Recommended: true},
			{ID: "b", Label: "B"},
		},
	}}
}

// promptsContainOutcomeRetry reports whether any chat prompt on the first
// sandbox looks like DefaultOutcomeRetry (the mis-kill injection).
func promptsContainOutcomeRetry(mgr *fakeManager) bool {
	b := mgr.bridge(0)
	if b == nil {
		return false
	}
	for i := 0; ; i++ {
		p := b.promptAt(i)
		if p == "" {
			return false
		}
		if strings.Contains(p, "立即调用 `node_complete`") || strings.Contains(p, models.DefaultOutcomeRetry) {
			return true
		}
	}
}

// TestReactAskQuestionNotKilledByForceOutcomeRetry locks the run-ac25c705
// regression: after ask_question (with recommended), force must return
// Questions/Done=false and must not inject DefaultOutcomeRetry.
func TestReactAskQuestionNotKilledByForceOutcomeRetry(t *testing.T) {
	qs := samplePendingQuestion()
	p, _, _, mgr, req := reactSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{narration: "need info", questions: qs}
			}
			// Reply turn still raises ask_question — previously force discarded
			// qs and finishReact→ensureOutcome injected OutcomeRetry.
			return turnAction{narration: "still clarifying", questions: qs}
		}
	})
	open := p.ReactOpen(context.Background(), req)
	if open.Done || len(open.Questions) == 0 {
		t.Fatalf("expected opening pause with questions, got Done=%v qs=%d", open.Done, len(open.Questions))
	}
	hist := []models.ReactMessage{{Role: "agent", Text: "need info"}, {Role: "human", Text: "force finish"}}
	reply := p.ReactReply(context.Background(), req, hist, "force finish", nil, true)
	if reply.Done {
		t.Fatal("pending ask_question must not finish under force (OutcomeRetry mis-kill)")
	}
	if len(reply.Questions) == 0 {
		t.Fatal("expected Questions returned for auto_clarify / waiting_human")
	}
	if promptsContainOutcomeRetry(mgr) {
		t.Error("DefaultOutcomeRetry must not be injected while ask_question is pending")
	}
	// Session must stay parked for the next human/auto turn.
	key := reactKey(req)
	p.mu.Lock()
	sess := p.sessions[key]
	p.mu.Unlock()
	if sess == nil {
		t.Fatal("react session must remain open when returning pending Questions")
	}
}

// TestReactAskQuestionSurvivesMaxRoundsCap: max_rounds touch with pending
// ask_question must return Questions, not discard then finishReact/ensure*.
func TestReactAskQuestionSurvivesMaxRoundsCap(t *testing.T) {
	qs := samplePendingQuestion()
	p, _, _, mgr, req := reactSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{narration: "need info", questions: qs}
			}
			return turnAction{narration: "one more question", questions: qs}
		}
	})
	req.Config["max_rounds"] = 1
	open := p.ReactOpen(context.Background(), req)
	if open.Done || len(open.Questions) == 0 {
		t.Fatalf("expected opening pause, got Done=%v qs=%d", open.Done, len(open.Questions))
	}
	// history has 1 human → humanTurns=2 >= max_rounds=1 ⇒ cap reached.
	hist := []models.ReactMessage{{Role: "agent", Text: "need info"}, {Role: "human", Text: "answer"}}
	reply := p.ReactReply(context.Background(), req, hist, "answer", nil, false)
	if reply.Done {
		t.Fatal("pending ask_question must outrank max_rounds finish")
	}
	if len(reply.Questions) == 0 {
		t.Fatal("expected Questions returned at cap")
	}
	if promptsContainOutcomeRetry(mgr) {
		t.Error("OutcomeRetry must not run when pending questions survive the cap")
	}
}

// TestReactPendingDuringEnsureStructuredReturnsQuestions: finish path with no
// initial qs enters ensureStructured; if the agent then ask_question, abort
// StructuredRetry/OutcomeRetry and return Questions (Done=false).
func TestReactPendingDuringEnsureStructuredReturnsQuestions(t *testing.T) {
	qs := samplePendingQuestion()
	p, _, _, mgr, req := reactSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				// No questions → finishReact → ensureStructured re-prompt.
				return turnAction{narration: "wrapping up"}
			}
			// StructuredRetry turn raises ask_question instead of set_*.
			return turnAction{narration: "need a decision first", questions: qs}
		}
	})
	open := p.ReactOpen(context.Background(), req)
	if open.Done {
		t.Fatal("pending raised during ensureStructured must not Done-finish")
	}
	if len(open.Questions) == 0 {
		t.Fatal("expected Questions from ensureStructured abort")
	}
	if promptsContainOutcomeRetry(mgr) {
		t.Error("must stop before OutcomeRetry when pending appears in ensureStructured")
	}
	key := reactKey(req)
	p.mu.Lock()
	sess := p.sessions[key]
	p.mu.Unlock()
	if sess == nil {
		t.Fatal("session must stay open after ensure* pending abort")
	}
}

func approveSetup(t *testing.T, chatFor func(attempt int) chatFunc) (*acpProvider, *mcp.Host, *memStore, *fakeManager, NodeReq) {
	t.Helper()
	p, host, store, mgr, req := reactSetup(t, chatFor)
	req.NodeType = "approve"
	req.Config = map[string]any{"agent_profile": "react-agent"}
	return p, host, store, mgr, req
}

func approveProduces() map[string]string {
	return map[string]string{
		mcp.ClarifiedRequirementArtifactName: mcp.MinimalValidClarifiedRequirementJSON,
		mcp.PlanArtifactName:                 `{"goals":[{"id":"g1","title":"目标"}]}`,
	}
}

func TestReactApproveOpenParksWithoutChat(t *testing.T) {
	chatted := false
	p, _, _, mgr, req := approveSetup(t, func(int) chatFunc {
		return func(int) turnAction {
			chatted = true
			t.Error("approve ReactOpen must not streamChat")
			return turnAction{}
		}
	})
	open := p.ReactOpen(context.Background(), req)
	if open.Done {
		t.Fatal("approve open must park, not finish")
	}
	if open.Msg != "" || len(open.Questions) != 0 {
		t.Fatalf("approve open must be empty idle, got msg=%q qs=%d", open.Msg, len(open.Questions))
	}
	if chatted {
		t.Fatal("approve open must not chat")
	}
	if mgr.createCount() != 1 {
		t.Errorf("create count = %d, want 1", mgr.createCount())
	}
	if mgr.bridge(0).promptAt(0) != "" {
		t.Fatalf("open must not send a prompt, got %q", mgr.bridge(0).promptAt(0))
	}
	if !p.HasLiveSession(req.RunID, req.NodeID) {
		t.Fatal("expected parked live session")
	}
}

func TestReactApproveFirstReplyInjectsContract(t *testing.T) {
	p, _, store, mgr, req := approveSetup(t, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{narration: "aligned", produces: approveProduces()}
		}
	})
	req.Vars = map[string]any{"feature": "邮箱验证码登录"}
	req.PromptImages = []models.PromptImage{{Data: "abc", MimeType: "image/png", Name: "shot.png"}}
	open := p.ReactOpen(context.Background(), req)
	if open.Done || len(open.Questions) != 0 {
		t.Fatalf("expected idle park, got Done=%v qs=%d", open.Done, len(open.Questions))
	}
	hist := []models.ReactMessage{{Role: "human", Text: "做邮箱登录"}}
	reply := p.ReactReply(context.Background(), req, hist, "做邮箱登录", nil, false)
	if reply.Err != nil {
		t.Fatalf("reply: %v", reply.Err)
	}
	prompt := mgr.bridge(0).promptAt(0)
	for _, want := range []string{"做邮箱登录", "## 用户消息", "set_clarified_requirement", "邮箱验证码登录", "真实分歧"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("first reply prompt missing %q:\n%s", want, prompt)
		}
	}
	if mgr.bridge(0).imageCountAt(0) != 1 {
		t.Fatalf("first reply should attach run var images, got %d", mgr.bridge(0).imageCountAt(0))
	}
	if strings.Contains(prompt, "本轮输出契约") {
		t.Fatal("opening approve turn must not append dual-write contract")
	}
	if reply.Done {
		t.Fatal("ordinary approve reply must not finish")
	}
	if _, ok := store.Get("run-r", mcp.ClarifiedRequirementArtifactName); !ok {
		t.Error("clarified requirement not written")
	}
	if _, ok := store.Get("run-r", mcp.PlanArtifactName); !ok {
		t.Error("plan not written")
	}
}

func TestReactApproveRehydrateSkipsPrime(t *testing.T) {
	p, _, _, mgr, req := approveSetup(t, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{narration: "aligned", produces: approveProduces()}
		}
	})
	open := p.ReactOpen(context.Background(), req)
	if open.Done {
		t.Fatal("expected park")
	}
	key := reactKey(req)
	p.mu.Lock()
	sess := p.sessions[key]
	p.mu.Unlock()
	if sess == nil {
		t.Fatal("expected live session")
	}
	sess.acp.Close()

	hist := []models.ReactMessage{{Role: "human", Text: "做邮箱登录"}}
	reply := p.ReactReply(context.Background(), req, hist, "做邮箱登录", nil, false)
	if reply.Err != nil {
		t.Fatalf("reply: %v", reply.Err)
	}
	if mgr.createCount() != 2 {
		t.Errorf("create count = %d, want 2 (open + rehydrate)", mgr.createCount())
	}
	prime := mgr.bridge(1).promptAt(0)
	if strings.Contains(prime, "会话恢复") {
		t.Errorf("approve first-turn rehydrate must not prime with recovery chat: %q", prime)
	}
	for _, want := range []string{"做邮箱登录", "set_clarified_requirement", "真实分歧"} {
		if !strings.Contains(prime, want) {
			t.Errorf("rehydrated first reply missing %q: %q", want, prime)
		}
	}
	if strings.Contains(prime, "本轮输出契约") {
		t.Errorf("opening approve turn must not append dual-write contract: %q", prime)
	}
}

func TestReactApproveRetryAfterFailedOpenStillInjects(t *testing.T) {
	p, _, _, mgr, req := approveSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{sendError: "boom"}
			}
			return turnAction{narration: "aligned", produces: approveProduces()}
		}
	})
	open := p.ReactOpen(context.Background(), req)
	if open.Done {
		t.Fatal("expected park")
	}
	first := p.ReactReply(context.Background(), req, []models.ReactMessage{{Role: "human", Text: "做登录"}}, "做登录", nil, false)
	if first.Err == nil && !strings.Contains(first.Msg, "澄清回复失败") {
		t.Fatalf("expected first reply to fail, got msg=%q err=%v", first.Msg, first.Err)
	}
	hist := []models.ReactMessage{
		{Role: "human", Text: "做登录"},
		{Role: "agent", Text: first.Msg},
		{Role: "human", Text: "再试一次"},
	}
	reply := p.ReactReply(context.Background(), req, hist, "再试一次", nil, false)
	if reply.Err != nil {
		t.Fatalf("retry: %v", reply.Err)
	}
	prompt := mgr.bridge(0).promptAt(1)
	for _, want := range []string{"再试一次", "set_clarified_requirement", "真实分歧"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("retry prompt missing %q:\n%s", want, prompt)
		}
	}
}

// TestReactApproveClearsPrematureOutcome locks g1.1: !force must ClearOutcome and
// stay Done=false even when the agent called node_complete during the turn.
func TestReactApproveClearsPrematureOutcome(t *testing.T) {
	p, host, _, _, req := approveSetup(t, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{narration: "aligned", produces: approveProduces(), outcome: true}
		}
	})
	open := p.ReactOpen(context.Background(), req)
	if open.Done {
		t.Fatal("expected park")
	}
	hist := []models.ReactMessage{{Role: "human", Text: "做登录"}}
	reply := p.ReactReply(context.Background(), req, hist, "做登录", nil, false)
	if reply.Err != nil {
		t.Fatalf("reply: %v", reply.Err)
	}
	if reply.Done {
		t.Fatal("ordinary approve reply must not finish even after premature node_complete")
	}
	if host.HasOutcome(req.RunID, req.NodeID) {
		t.Fatal("premature node_complete must be cleared on !force approve")
	}
	if _, st := host.PeekOutcomeArtifact(req.RunID); st != mcp.OutcomeArtifactAbsent {
		t.Fatalf("audit node_complete.json must be cleared, state=%v", st)
	}
}

// TestReactApproveForceCompletesWithOutcome locks g1.2: force runs
// ensureRequiredProducts + ensureOutcome; products + node_complete → Done.
func TestReactApproveForceCompletesWithOutcome(t *testing.T) {
	p, host, store, mgr, req := approveSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			// turn 0: ordinary align; turn 1: force confirm with products+outcome
			if turn == 0 {
				return turnAction{narration: "aligned", produces: approveProduces()}
			}
			return turnAction{narration: "finishing", produces: approveProduces(), outcome: true}
		}
	})
	_ = p.ReactOpen(context.Background(), req)
	hist := []models.ReactMessage{{Role: "human", Text: "做登录"}}
	idle := p.ReactReply(context.Background(), req, hist, "做登录", nil, false)
	if idle.Done {
		t.Fatal("pre-confirm reply must stay open")
	}
	hist = append(hist,
		models.ReactMessage{Role: "agent", Text: idle.Msg},
		models.ReactMessage{Role: "human", Text: "确认并流转"},
	)
	force := p.ReactReply(context.Background(), req, hist, "确认并流转", nil, true)
	if force.Err != nil {
		t.Fatalf("force: %v", force.Err)
	}
	if !force.Done {
		t.Fatalf("force with products+outcome must complete, got %+v", force)
	}
	if !host.HasOutcome(req.RunID, req.NodeID) {
		t.Fatal("force path must keep node_complete mark")
	}
	for _, name := range []string{mcp.ClarifiedRequirementArtifactName, mcp.PlanArtifactName} {
		if _, ok := store.Get("run-r", name); !ok {
			t.Errorf("missing %s after force", name)
		}
	}
	prompt := mgr.bridge(0).promptAt(1)
	for _, want := range []string{"确认流转", "node_complete", "set_clarified_requirement", "set_plan",
		"平台已核对", "不要重复写入产物"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("force prompt missing %q:\n%s", want, prompt)
		}
	}
}

// TestReactApproveForceOmitsReadyNoteWhenProductsIncomplete: missing plan or
// leftover open_questions keep the confirm prompt at the full reconcile wording.
func TestReactApproveForceOmitsReadyNoteWhenProductsIncomplete(t *testing.T) {
	p, _, _, mgr, req := approveSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{narration: "aligned", produces: map[string]string{
					mcp.ClarifiedRequirementArtifactName: mcp.MinimalValidClarifiedRequirementJSON,
				}}
			}
			return turnAction{narration: "finishing", produces: approveProduces(), outcome: true}
		}
	})
	_ = p.ReactOpen(context.Background(), req)
	hist := []models.ReactMessage{{Role: "human", Text: "做登录"}}
	idle := p.ReactReply(context.Background(), req, hist, "做登录", nil, false)
	if idle.Done {
		t.Fatal("pre-confirm reply must stay open")
	}
	hist = append(hist,
		models.ReactMessage{Role: "agent", Text: idle.Msg},
		models.ReactMessage{Role: "human", Text: "确认并流转"},
	)
	force := p.ReactReply(context.Background(), req, hist, "确认并流转", nil, true)
	if force.Err != nil {
		t.Fatalf("force: %v", force.Err)
	}
	prompt := mgr.bridge(0).promptAt(1)
	if strings.Contains(prompt, "平台已核对") {
		t.Fatalf("incomplete products must not get the ready note:\n%s", prompt)
	}
	for _, want := range []string{"确认流转", "node_complete", "set_clarified_requirement", "set_plan"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("force prompt missing %q:\n%s", want, prompt)
		}
	}
}

// TestReactApproveFirstForceInjectsReadyNoteWhenAlreadySettled: first-message
// confirm (open-prompt + force) still appends the skip-rewrite note when the
// store already holds both products.
func TestReactApproveFirstForceInjectsReadyNoteWhenAlreadySettled(t *testing.T) {
	p, host, _, mgr, req := approveSetup(t, func(int) chatFunc {
		return func(int) turnAction {
			return turnAction{narration: "finishing", produces: approveProduces(), outcome: true}
		}
	})
	tok := req.Token
	if _, err := host.WriteArtifact(req.RunID, tok, req.NodeID, mcp.ClarifiedRequirementArtifactName,
		mcp.MinimalValidClarifiedRequirementJSON, "json"); err != nil {
		t.Fatal(err)
	}
	if _, err := host.WriteArtifact(req.RunID, tok, req.NodeID, mcp.PlanArtifactName,
		`{"goals":[{"id":"g1","title":"目标"}]}`, "json"); err != nil {
		t.Fatal(err)
	}
	_ = p.ReactOpen(context.Background(), req)
	hist := []models.ReactMessage{{Role: "human", Text: "确认并流转"}}
	force := p.ReactReply(context.Background(), req, hist, "确认并流转", nil, true)
	if force.Err != nil {
		t.Fatalf("force: %v", force.Err)
	}
	prompt := mgr.bridge(0).promptAt(0)
	for _, want := range []string{"确认流转", "平台已核对", "不要重复写入产物", "## 用户消息"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("first-message force prompt missing %q:\n%s", want, prompt)
		}
	}
}

// The summary contract is gone from ordinary ReAct turns: those now carry the
// human's message verbatim, and only「确认并流转」appends the reconcile
// instruction plus a hidden summary turn whose JSON never reaches the bubble.
func TestReactSummaryOnlyRunsOnConfirmTurn(t *testing.T) {
	const summary = "用户确认按截图保留视觉,去掉下拉与紫色选中。"
	p, _, _, mgr, req := approveSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			switch turn {
			case 0, 1:
				return turnAction{narration: "aligned", produces: approveProduces()}
			case 2:
				return turnAction{narration: "已按聊天记录补齐产物",
					produces: approveProduces(), outcome: true}
			default:
				return turnAction{narration: "```json\n{\"agentSummary\":\"" + summary + "\"}\n```"}
			}
		}
	})
	_ = p.ReactOpen(context.Background(), req)

	hist := []models.ReactMessage{{Role: "human", Text: "做登录"}}
	opening := p.ReactReply(context.Background(), req, hist, "做登录", nil, false)
	hist = append(hist, models.ReactMessage{Role: "agent", Text: opening.Msg})

	hist = append(hist, models.ReactMessage{Role: "human", Text: "去掉下拉"})
	idle := p.ReactReply(context.Background(), req, hist, "去掉下拉", nil, false)
	if idle.AgentSummary != "" {
		t.Fatalf("ordinary ReAct turns must not produce a summary, got %q", idle.AgentSummary)
	}
	if got := mgr.bridge(0).promptAt(1); got != "去掉下拉" {
		t.Fatalf("ordinary turn must send the human message verbatim, got:\n%s", got)
	}

	hist = append(hist,
		models.ReactMessage{Role: "agent", Text: idle.Msg},
		models.ReactMessage{Role: "human", Text: "确认并流转"},
	)
	force := p.ReactReply(context.Background(), req, hist, "确认并流转", nil, true)
	if force.Err != nil {
		t.Fatalf("force: %v", force.Err)
	}
	if force.AgentSummary != summary {
		t.Fatalf("confirm summary = %q, want %q", force.AgentSummary, summary)
	}
	if force.Msg != "已按聊天记录补齐产物" {
		t.Fatalf("the summary turn must stay out of the bubble, got %q", force.Msg)
	}
	reconcile := mgr.bridge(0).promptAt(2)
	for _, want := range []string{"确认流转", "完整聊天记录", "set_plan", "确认并流转"} {
		if !strings.Contains(reconcile, want) {
			t.Fatalf("reconcile prompt missing %q:\n%s", want, reconcile)
		}
	}
	if strings.Contains(reconcile, "agentSummary") {
		t.Fatalf("the reconcile turn must not carry the summary contract:\n%s", reconcile)
	}
	summaryPrompt := mgr.bridge(0).promptAt(3)
	for _, want := range []string{"agentSummary", "只输出一个 fenced JSON"} {
		if !strings.Contains(summaryPrompt, want) {
			t.Fatalf("summary prompt missing %q:\n%s", want, summaryPrompt)
		}
	}
}

// A confirm the platform is about to REJECT (agent still has open questions)
// must not spend a summary turn: the induction only makes sense once the node is
// really finishing, and running it earlier both delays the rejection and risks
// coming between the reconcile turn and the node_complete gate.
func TestReactRejectedConfirmSkipsSummaryTurn(t *testing.T) {
	qs := samplePendingQuestion()
	p, _, _, mgr, req := reactSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{narration: "need info", questions: qs}
			}
			// Confirm turn + every ensure re-prompt keeps asking, so finishReact
			// returns Questions/Done=false.
			return turnAction{narration: "still clarifying", questions: qs}
		}
	})
	_ = p.ReactOpen(context.Background(), req)
	hist := []models.ReactMessage{{Role: "human", Text: "做登录"}}
	force := p.ReactReply(context.Background(), req, hist, "确认并流转", nil, true)
	if force.Done {
		t.Fatalf("a confirm with open questions must stay open: %+v", force)
	}
	if force.AgentSummary != "" {
		t.Fatalf("rejected confirm must not carry a summary, got %q", force.AgentSummary)
	}
	for i := 0; ; i++ {
		prompt := mgr.bridge(0).promptAt(i)
		if prompt == "" {
			break
		}
		if strings.Contains(prompt, "只输出一个 fenced JSON") {
			t.Fatalf("summary turn must not run on a rejected confirm (prompt %d):\n%s", i, prompt)
		}
	}
}

// A summary turn that answers with prose instead of the JSON contract costs the
// ledger its summary; it must never block the transition.
func TestReactConfirmSurvivesUnparseableSummary(t *testing.T) {
	p, host, _, _, req := approveSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			switch turn {
			case 0:
				return turnAction{narration: "aligned", produces: approveProduces()}
			case 1:
				return turnAction{narration: "已核对产物", produces: approveProduces(), outcome: true}
			default:
				return turnAction{narration: "本轮没有可归纳的内容。"}
			}
		}
	})
	_ = p.ReactOpen(context.Background(), req)
	hist := []models.ReactMessage{{Role: "human", Text: "做登录"}}
	idle := p.ReactReply(context.Background(), req, hist, "做登录", nil, false)
	hist = append(hist,
		models.ReactMessage{Role: "agent", Text: idle.Msg},
		models.ReactMessage{Role: "human", Text: "确认并流转"},
	)
	force := p.ReactReply(context.Background(), req, hist, "确认并流转", nil, true)
	if force.Err != nil {
		t.Fatalf("force: %v", force.Err)
	}
	if !force.Done {
		t.Fatalf("an unparseable summary must not hold the node open: %+v", force)
	}
	if force.AgentSummary != "" {
		t.Fatalf("prose must not be promoted to a summary, got %q", force.AgentSummary)
	}
	if !host.HasOutcome(req.RunID, req.NodeID) {
		t.Fatal("force path must keep node_complete mark")
	}
}

// TestReactApproveForceNudgesMissingOutcome locks g1.2: force without
// node_complete injects OutcomeRetry via ensureOutcome.
func TestReactApproveForceNudgesMissingOutcome(t *testing.T) {
	p, host, _, mgr, req := approveSetup(t, func(int) chatFunc {
		return func(turn int) turnAction {
			if turn == 0 {
				return turnAction{narration: "aligned", produces: approveProduces()}
			}
			// Force turn + ensureOutcome retries: products only, never outcome.
			return turnAction{narration: "still no mark", produces: approveProduces()}
		}
	})
	_ = p.ReactOpen(context.Background(), req)
	hist := []models.ReactMessage{{Role: "human", Text: "做登录"}}
	idle := p.ReactReply(context.Background(), req, hist, "做登录", nil, false)
	hist = append(hist,
		models.ReactMessage{Role: "agent", Text: idle.Msg},
		models.ReactMessage{Role: "human", Text: "确认并流转"},
	)
	force := p.ReactReply(context.Background(), req, hist, "确认并流转", nil, true)
	if force.Err != nil {
		t.Fatalf("force: %v", force.Err)
	}
	if !force.Done {
		t.Fatal("ensureOutcome fail-closed still returns Done so engine can reject missing mark")
	}
	if host.HasOutcome(req.RunID, req.NodeID) {
		t.Fatal("outcome must remain absent when agent never calls node_complete")
	}
	if !promptsContainOutcomeRetry(mgr) {
		t.Fatal("force without node_complete must inject OutcomeRetry")
	}
}
