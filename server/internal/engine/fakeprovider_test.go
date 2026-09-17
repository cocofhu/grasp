package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

// fakeProvider is a deterministic test double for runtime.ExecProvider used
// only by the FSM engine unit tests (no Docker / network). It writes the
// declared produces artifact through the run-scoped MCP host, exactly as the
// real provider harvests one from the sandbox, so the produces-contract and
// FSM routing logic can be exercised offline. This lives in _test code only;
// the product ships no mock execution.
type fakeProvider struct {
	host *mcp.Host

	// Test-injectable failure script (default off): failLeft[nodeID] forced
	// failures with reason, so rollback / routeFailure / last_error carry can
	// be exercised. seenVars records the vars each node saw for assertions.
	mu       sync.Mutex
	failLeft map[string]int
	reason   string
	seenVars map[string]map[string]any
	// promptImages records PromptImages from each node for integration tests.
	promptImages map[string][]models.PromptImage

	// react controls (test-only): reactPending keeps the dialogue open for N
	// more replies (each returns Done:false with a follow-up question);
	// reactSkipProduces makes the final turn write no clarified_requirement so
	// the produces contract fails and routeFailure is exercised.
	reactPending      int
	reactSkipProduces bool
	// approveSkipPlan writes clarified_requirement.json but not plan.json so
	// the Approve two-product contract fails.
	approveSkipPlan bool
	// approveWriteOptional also writes research.json so optional lift is tested.
	approveWriteOptional bool
	// reactForceStayOpen: force=true still returns Done:false (pending ask_question).
	reactForceStayOpen bool
	// reactSetupErr, when set, makes ReactOpen fail with a sandbox setup error.
	reactSetupErr error

	// submit_mr controls (test-only): optional mr_url reported via node_complete
	// outputs (platform no longer gates on pushed / conflicts).
	mrURL string
	// mrRepoCalls records config["repo"] for each submit_mr RunAgent call (order).
	mrRepoCalls []string
	// mrSourceCalls / mrTargetCalls record interpolated branch config per call.
	mrSourceCalls []string
	mrTargetCalls []string
	// mrFailOnRepo, when set, makes the call for that pinned repo fail.
	mrFailOnRepo string
	// mrURLByRepo optionally overrides mrURL per pinned repo name.
	mrURLByRepo map[string]string

	// skipOutcome (test-only): when true, do not call node_complete so the
	// engine's missing-mark path can be exercised.
	skipOutcome bool
	// skipOutcomeLeft (test-only): when > 0, skip node_complete for that many
	// RunAgent calls (decrementing), then resume normal emitOutcome. Used to
	// exercise empty-MCP-surface auto-retry recovery.
	skipOutcomeLeft int
	// outcomeFailed (test-only): node_complete with status=failed.
	outcomeFailed bool
	// outcomeBeforeFail (test-only): when a forced failLeft failure fires,
	// emit node_complete first so tests can assert stale marks are cleared
	// before the next attempt (RunAgent error skips TakeOutcome).
	outcomeBeforeFail bool

	// visual control (test-only): when true a visual node finishes without
	// writing page.html, so execVisual's contract-miss path is exercised.
	visualSkipProduces bool
	// visualBodyByNode optionally overrides the HTML written for a visual node
	// so dual-visual independence tests can assert distinct pages.
	visualBodyByNode map[string]string

	// structuredSkipProduces (test-only): when true a framework node finishes
	// without writing its reserved structured JSON, so finalizeStructured's
	// contract-miss path (retryable=false) can be exercised.
	structuredSkipProduces bool

	// recordCalls (test-only): when true the fake issues a real built-in MCP
	// tool call through the run-scoped JSON-RPC endpoint (ServeRPC) on each turn,
	// so the recorded per-node MCP call trace (StateRun.McpCalls) can be asserted
	// end-to-end. Off by default so other engine tests are unaffected.
	recordCalls bool

	// structuredBodies overrides the default fake JSON for a node id (test/review).
	structuredBodies map[string]string

	// structuredBodySeq (test-only) yields one body per RunAgent call for a node
	// id, so a retried framework node writes a DIFFERENT structured product on
	// each attempt (e.g. fail-with-screenshots then pass-with-screenshots),
	// exercising per-iteration output snapshots. Drains to structuredBodies / the
	// default once the sequence is exhausted.
	structuredBodySeq map[string][]string

	// failWithEvents (test-only): when RunAgent fails, attach these events to
	// NodeResult so engine err-path Events persist can be asserted.
	failWithEvents []models.AcpEvent

	// review controls (test-only): reviseCalls counts ReviseInPlace per node id;
	// retired records nodes whose parked session was released.
	reviseCalls map[string]int
	retired     map[string]bool
	// wrapUpCalls counts OfferCommitOnConfirm per node id; wrapUpAfterRetire
	// is set if wrap-up ran after RetireSession (ordering bug).
	wrapUpCalls       map[string]int
	wrapUpAfterRetire bool
	wrapUpMsg         string
	// reconcileCalls counts ReconcileOnConfirm per node id; wrapUpBeforeReconcile
	// is set if the git wrap-up ran first (ordering bug: reconcile must land the
	// product edits before anything is committed).
	reconcileCalls        map[string]int
	wrapUpBeforeReconcile bool
	reconcileMsg          string
	reconcileSummary      string
	// reactReplyCalls counts ReactReply invocations per node id (assert review
	// force no longer routes through Agent wrap-up).
	reactReplyCalls map[string]int
	// reactConfirmSummary (test-only): the AgentSummary a force ReactReply
	// returns, standing in for the real provider's hidden summary turn.
	reactConfirmSummary string
	// reviseErr (test-only): when set, ReviseInPlace returns this error (and
	// optionally skips writing products when reviseSkipWrite is true).
	reviseErr       error
	reviseSkipWrite bool

	// agentUsage (test-only): when set, RunAgent attaches this Usage so review
	// enter/saveState paths can assert StateRun.Usage is preserved across pause.
	agentUsage *models.TokenUsage
	// reviseUsage (test-only): when set, ReviseInPlace returns this Usage delta
	// so flushTokenUsage on review/gate-react revise turns can be asserted.
	reviseUsage *models.TokenUsage
	// reviseHold (test-only): when non-nil, ReviseInPlace blocks until the
	// channel is closed (simulates a long in-flight turn for Cancel/ready tests).
	reviseHold <-chan struct{}
	// reactHold (test-only): same for ReactReply / clarify session Cancel.
	reactHold <-chan struct{}
}

// nextStructuredBody returns the reserved-artifact body to write for nodeID on
// this call: the next scripted sequence entry when present, else the static
// per-node override, else the caller's default.
func (f *fakeProvider) nextStructuredBody(nodeID, def string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if seq := f.structuredBodySeq[nodeID]; len(seq) > 0 {
		body := seq[0]
		f.structuredBodySeq[nodeID] = seq[1:]
		return body
	}
	if b, ok := f.structuredBodies[nodeID]; ok {
		return b
	}
	return def
}

func (f *fakeProvider) Name() string { return "fake-test" }

// emitCall issues one recorded list_artifacts call via the JSON-RPC endpoint so
// it is attributed to the currently-active node and lands on that execution's
// StateRun.McpCalls (only when recordCalls is enabled).
func (f *fakeProvider) emitCall(req runtime.NodeReq) {
	if !f.recordCalls {
		return
	}
	f.host.ServeRPC(req.RunID, req.Token, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_artifacts","arguments":{}}}`))
}

// emitAsk issues a recorded ask_question call via the JSON-RPC endpoint. Besides
// recording the trace, callTool sets the pending questions, so react turns can
// Take them exactly as the direct SetPendingQuestions path does.
func (f *fakeProvider) emitAsk(req runtime.NodeReq) {
	f.host.ServeRPC(req.RunID, req.Token, []byte(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask_question","arguments":{"questions":[{"prompt":"补充?","options":["A","B"]}]}}}`))
}

// emitOutcome records node_complete for the active node (unless skipOutcome /
// skipOutcomeLeft suppress it).
func (f *fakeProvider) emitOutcome(req runtime.NodeReq, outputs map[string]any) {
	f.mu.Lock()
	skip := f.skipOutcome
	if !skip && f.skipOutcomeLeft > 0 {
		f.skipOutcomeLeft--
		skip = true
	}
	failed := f.outcomeFailed
	f.mu.Unlock()
	if skip {
		return
	}
	status := "success"
	args := map[string]any{"status": status, "summary": "fake ok"}
	if failed {
		args["status"] = "failed"
		args["error"] = "fake outcome failed"
		args["summary"] = "fake outcome failed"
	}
	if len(outputs) > 0 {
		args["outputs"] = outputs
	}
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "node_complete", "arguments": args},
	})
	f.host.ServeRPC(req.RunID, req.Token, body)
}

func (f *fakeProvider) recordAndMaybeFail(req runtime.NodeReq) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seenVars == nil {
		f.seenVars = map[string]map[string]any{}
	}
	snap := map[string]any{}
	for k, v := range req.Vars {
		snap[k] = v
	}
	f.seenVars[req.NodeID] = snap
	if f.promptImages == nil {
		f.promptImages = map[string][]models.PromptImage{}
	}
	if len(req.PromptImages) > 0 {
		copied := make([]models.PromptImage, len(req.PromptImages))
		copy(copied, req.PromptImages)
		f.promptImages[req.NodeID] = copied
	}
	if f.failLeft != nil && f.failLeft[req.NodeID] > 0 {
		f.failLeft[req.NodeID]--
		reason := f.reason
		if reason == "" {
			reason = "forced failure"
		}
		return fmt.Errorf("%s", reason)
	}
	return nil
}

func (f *fakeProvider) varsFor(nodeID string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.seenVars[nodeID]
}

func (f *fakeProvider) lastPromptImages(nodeID string) []models.PromptImage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.promptImages[nodeID]
}

func (f *fakeProvider) RunAgent(ctx context.Context, req runtime.NodeReq) (runtime.NodeResult, error) {
	if err := f.recordAndMaybeFail(req); err != nil {
		f.mu.Lock()
		beforeFail := f.outcomeBeforeFail
		// Stale-mark tests set skipOutcome for the success attempt; still emit
		// on the failing attempt so the buffer is non-empty across the retry.
		savedSkip := f.skipOutcome
		if beforeFail {
			f.skipOutcome = false
		}
		f.mu.Unlock()
		if beforeFail {
			f.emitOutcome(req, map[string]any{"stale": true})
			f.mu.Lock()
			f.skipOutcome = savedSkip
			f.mu.Unlock()
		}
		f.mu.Lock()
		events := f.failWithEvents
		f.mu.Unlock()
		return runtime.NodeResult{Events: events}, err
	}
	f.emitCall(req)
	content := fmt.Sprintf("# %s (fake)\n\nprofile=%v\n", req.NodeID, req.Config["agent_profile"])
	out := map[string]any{"content": content}
	// Record the resolved prompt so tests can assert conditional injection.
	out["prompt"] = fakePrompt(req)
	// implement nodes publish a name→branch map (exportBranchVar consumes it).
	if req.NodeType == "implement" {
		out["branches"] = `{"app":"feature/impl"}`
	}
	// submit_mr: record config and prepare mr_url for node_complete outputs.
	var outcomeOut map[string]any
	if req.NodeType == "submit_mr" {
		repo, _ := req.Config["repo"].(string)
		src, _ := req.Config["source_branch"].(string)
		tgt, _ := req.Config["target_branch"].(string)
		f.mu.Lock()
		f.mrRepoCalls = append(f.mrRepoCalls, repo)
		f.mrSourceCalls = append(f.mrSourceCalls, src)
		f.mrTargetCalls = append(f.mrTargetCalls, tgt)
		failOn := f.mrFailOnRepo
		url := f.mrURL
		if f.mrURLByRepo != nil {
			if u, ok := f.mrURLByRepo[repo]; ok {
				url = u
			}
		}
		f.mu.Unlock()
		if failOn != "" && repo == failOn {
			return runtime.NodeResult{}, fmt.Errorf("forced failure on repo %s", repo)
		}
		out["mr_url"] = url
		out["branch"] = "feature/impl"
		outcomeOut = map[string]any{"mr_url": url, "branch": "feature/impl"}
	}
	if produces, _ := req.Config["produces"].(string); produces != "" {
		body := content
		if strings.HasSuffix(produces, ".json") {
			body = `{"success": true, "complete": true}`
		}
		id, err := f.host.WriteArtifact(req.RunID, req.Token, req.NodeID, produces, body, kindOf(produces))
		if err != nil {
			return runtime.NodeResult{}, err
		}
		out["artifact_id"] = id
	}
	// app_preview requires at least one set_preview registration before the
	// engine will open the human approval gate.
	if req.NodeType == "app_preview" && f.host != nil {
		f.host.PutPreviewPortForTest(req.RunID, req.NodeID, 5173, "前端")
	}
	// visual nodes produce page.html only through write_artifact (no workspace
	// file), mirroring the real contract; visualSkipProduces omits it to exercise
	// the contract-miss path.
	if req.NodeType == "visual" && !f.visualSkipProduces {
		body := "<!doctype html><html><head><style>body{color:red}</style></head><body><h1>demo</h1></body></html>"
		if f.visualBodyByNode != nil {
			if custom, ok := f.visualBodyByNode[req.NodeID]; ok && custom != "" {
				body = custom
			}
		}
		id, err := f.host.WriteArtifact(req.RunID, req.Token, req.NodeID, visualPageName, body, "html")
		if err != nil {
			return runtime.NodeResult{}, err
		}
		out["artifact_id"] = id
	}
	// Framework nodes with a reserved structured product: write a minimal valid
	// one so the engine's structured-contract enforcement is satisfied offline.
	// structuredSkipProduces omits it to exercise finalizeStructured's miss path.
	if name, body := fakeStructured(req.NodeType); name != "" && !f.structuredSkipProduces {
		body = f.nextStructuredBody(req.NodeID, body)
		if _, err := f.host.WriteArtifact(req.RunID, req.Token, req.NodeID, name, body, "json"); err != nil {
			return runtime.NodeResult{}, err
		}
	}
	f.emitOutcome(req, outcomeOut)
	f.mu.Lock()
	usage := models.CloneTokenUsage(f.agentUsage)
	f.mu.Unlock()
	return runtime.NodeResult{OutputMd: content, Outputs: out, Usage: usage}, nil
}

// fakePrompt mirrors the provider's conditional_prompt injection so engine
// tests can assert the injected text reaches the agent when when_var is set.
func fakePrompt(req runtime.NodeReq) string {
	p, _ := req.Config["prompt"].(string)
	if cp, ok := req.Config["conditional_prompt"].(map[string]any); ok {
		whenVar, _ := cp["when_var"].(string)
		text, _ := cp["text"].(string)
		if whenVar != "" && text != "" {
			if v, ok := req.Vars[whenVar]; ok && fmt.Sprint(v) != "" && fmt.Sprint(v) != "false" {
				p += "\n\n" + text
			}
		}
	}
	return p
}

// fakeStructured returns the reserved artifact name and a minimal valid JSON
// body for the autonomous framework-card node types.
func fakeStructured(nodeType string) (name, body string) {
	switch nodeType {
	case "plan":
		return mcp.PlanArtifactName, `{"goals":[{"id":"g1","title":"目标","status":"done"}]}`
	case "implement":
		return mcp.ImplementationResultArtifactName, `{"summary":"done"}`
	case "research":
		return mcp.ResearchArtifactName, `{"summary":"researched","findings":[{"id":"r1","title":"f"}]}`
	case "test":
		return mcp.TestResultArtifactName, `{"summary":"tested","passed":1,"failed":0,"skipped":0}`
	case "review":
		return mcp.ReviewArtifactName, `{"summary":"reviewed","verdict":"approve"}`
	case "proposal":
		return mcp.ProposalsArtifactName, `{"context":"ctx","proposals":[{"id":"p1","title":"A"},{"id":"p2","title":"B","recommended":true}]}`
	}
	return "", ""
}

// ReactOpen raises one structured question so the dialogue pauses for a human
// reply (mirrors an agent calling ask_question on its opening turn).
func (f *fakeProvider) ReactOpen(ctx context.Context, req runtime.NodeReq) runtime.ReactTurn {
	f.mu.Lock()
	setupErr := f.reactSetupErr
	f.mu.Unlock()
	if setupErr != nil {
		return runtime.ReactTurn{
			SetupErr: setupErr,
			Msg:      "(沙箱启动失败,无法开始澄清:" + setupErr.Error() + ")",
			Events:   []models.AcpEvent{{Kind: "message", Text: "react open failed: " + setupErr.Error()}},
		}
	}
	if req.NodeType == "approve" {
		return runtime.ReactTurn{}
	}
	if f.recordCalls {
		f.emitAsk(req) // records the call + sets the pending questions
	} else {
		f.host.SetPendingQuestions(req.RunID, req.NodeID, []models.ReactQuestion{{
			ID: "q1", Prompt: "请补充关键信息。",
			Options: []models.ReactOption{{ID: "o1", Label: "选项A"}, {ID: "o2", Label: "选项B"}},
		}})
	}
	qs := f.host.TakePendingQuestions(req.RunID, req.NodeID)
	return runtime.ReactTurn{Msg: "请补充关键信息。", Questions: qs,
		Events: []models.AcpEvent{{Kind: "message", Text: "react open"}}}
}

// ReactReply concludes the clarification with no further questions (or on a
// forced finish), writing the declared produces artifact like the real provider
// harvests one.
func (f *fakeProvider) ReactReply(ctx context.Context, req runtime.NodeReq, history []models.ReactMessage, human string, images []models.PromptImage, force bool) runtime.ReactTurn {
	f.mu.Lock()
	if f.reactReplyCalls == nil {
		f.reactReplyCalls = map[string]int{}
	}
	f.reactReplyCalls[req.NodeID]++
	pending := f.reactPending
	if pending > 0 && !force {
		f.reactPending--
	}
	skip := f.reactSkipProduces
	stayOpen := f.reactForceStayOpen
	hold := f.reactHold
	f.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return runtime.ReactTurn{Msg: "(已中断)", Done: false, Err: ctx.Err(),
				Events: []models.AcpEvent{{Kind: "message", Text: "clarify-cancelled"}}}
		}
	}
	if stayOpen {
		if f.recordCalls {
			f.emitAsk(req)
		} else {
			f.host.SetPendingQuestions(req.RunID, req.NodeID, []models.ReactQuestion{{ID: "q-open", Prompt: "仍有待拍板问题。"}})
		}
		qs := f.host.TakePendingQuestions(req.RunID, req.NodeID)
		return runtime.ReactTurn{Msg: "仍有待拍板问题。", Questions: qs, Done: false,
			Events: []models.AcpEvent{{Kind: "message", Text: "force-stay-open"}}}
	}
	// Still gathering info: keep the dialogue open with another question.
	if pending > 0 && !force {
		if f.recordCalls {
			f.emitAsk(req)
		} else {
			f.host.SetPendingQuestions(req.RunID, req.NodeID, []models.ReactQuestion{{ID: "q2", Prompt: "还需要一点信息。"}})
		}
		qs := f.host.TakePendingQuestions(req.RunID, req.NodeID)
		return runtime.ReactTurn{Msg: "请继续补充。", Questions: qs, Done: false,
			Events: []models.AcpEvent{{Kind: "message", Text: "follow-up"}}}
	}
	content := "## 结论\n\n" + human
	if skip {
		if req.NodeType == "approve" && !force {
			f.host.ClearOutcome(req.RunID, req.NodeID)
			return runtime.ReactTurn{Msg: "完成(缺产物)。", Done: false,
				Result: runtime.NodeResult{OutputMd: content, Outputs: map[string]any{}}}
		}
		// Finish without writing the reserved product -> contract miss.
		// Still mark outcome so the engine reaches the produces check.
		f.emitOutcome(req, nil)
		return runtime.ReactTurn{Msg: "完成(缺产物)。", Done: true,
			Result: runtime.NodeResult{OutputMd: content, Outputs: map[string]any{}}}
	}
	out := map[string]any{"clarified_requirement": content}
	// A react node's structured deliverable is clarified_requirement.json.
	body := fmt.Sprintf(`{
		"title":"需求","summary":%q,"background":"测试背景",
		"goals":["完成需求"],"in_scope":["本功能"],"out_of_scope":["其它"],
		"functional_requirements":[{"id":"f1","title":"需求","detail":"实现所述需求","priority":"must","acceptance_criteria":["可验收"]}],
		"assumptions":["无额外假设(已与用户确认)"],"dependencies":["无额外依赖(已与用户确认)"],"constraints":["无额外约束(已与用户确认)"]
	}`, human)
	id, err := f.host.WriteArtifact(req.RunID, req.Token, req.NodeID, mcp.ClarifiedRequirementArtifactName, body, "json")
	if err != nil {
		// Surface write failures instead of silently failing the produces contract.
		return runtime.ReactTurn{Msg: "完成(写产物失败:" + err.Error() + ")。", Done: true,
			Result: runtime.NodeResult{OutputMd: content, Outputs: map[string]any{}}}
	}
	out["artifact_id"] = id
	if req.NodeType == "approve" {
		f.mu.Lock()
		skipPlan := f.approveSkipPlan
		writeOpt := f.approveWriteOptional
		f.mu.Unlock()
		if !skipPlan {
			_, planBody := fakeStructured("plan")
			if _, err := f.host.WriteArtifact(req.RunID, req.Token, req.NodeID, mcp.PlanArtifactName, planBody, "json"); err != nil {
				return runtime.ReactTurn{Msg: "完成(写计划失败:" + err.Error() + ")。", Done: true,
					Result: runtime.NodeResult{OutputMd: content, Outputs: out}}
			}
		}
		if writeOpt {
			_, researchBody := fakeStructured("research")
			if _, err := f.host.WriteArtifact(req.RunID, req.Token, req.NodeID, mcp.ResearchArtifactName, researchBody, "json"); err != nil {
				return runtime.ReactTurn{Msg: "完成(写调研失败:" + err.Error() + ")。", Done: true,
					Result: runtime.NodeResult{OutputMd: content, Outputs: out}}
			}
		}
	}
	if req.NodeType == "approve" && !force {
		// Mirror real acpProvider: discard premature node_complete so !force
		// cannot complete the node (ClearOutcome + Done=false).
		f.host.ClearOutcome(req.RunID, req.NodeID)
		f.host.SetOutcomeAllowed(req.RunID, false)
		return runtime.ReactTurn{Msg: "信息已充分。", Done: false, Result: runtime.NodeResult{OutputMd: content, Outputs: out}}
	}
	if req.NodeType == "approve" || req.NodeType == "grasp" {
		f.host.SetOutcomeAllowed(req.RunID, true)
	}
	f.emitOutcome(req, nil)
	turn := runtime.ReactTurn{Msg: "信息已充分。", Done: true,
		Result: runtime.NodeResult{OutputMd: content, Outputs: out}}
	if force {
		// Only the confirm turn runs the hidden summary turn.
		f.mu.Lock()
		turn.AgentSummary = f.reactConfirmSummary
		f.mu.Unlock()
	}
	return turn
}

// --- runtime.ReviewProvider (post-run ReAct review phase) --------------------
// The fake keeps a parked "session" per node id (a bool) so HasLiveSession is
// meaningful. ReviseInPlace re-writes the node's reserved structured product
// (like the agent rewriting it via set_*) and stays non-Done, mirroring the
// real provider's keep-editing turn.

func (f *fakeProvider) parkKey(runID, nodeID string) string { return runID + "|" + nodeID }

func (f *fakeProvider) ReviseInPlace(ctx context.Context, req runtime.NodeReq, history []models.ReactMessage, human string, images []models.PromptImage) runtime.ReactTurn {
	f.mu.Lock()
	skipWrite := f.reviseSkipWrite
	revErr := f.reviseErr
	hold := f.reviseHold
	if f.reviseCalls == nil {
		f.reviseCalls = map[string]int{}
	}
	f.reviseCalls[req.NodeID]++
	f.mu.Unlock()

	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return runtime.ReactTurn{Msg: "(已中断)", Done: false, Err: ctx.Err(),
				Events: []models.AcpEvent{{Kind: "message", Text: "revise-cancelled"}}}
		}
	}

	if !skipWrite {
		if name, body := fakeStructured(req.NodeType); name != "" {
			body = f.nextStructuredBody(req.NodeID, body)
			_, _ = f.host.WriteArtifact(req.RunID, req.Token, req.NodeID, name, body, "json")
		}
		// Visual review rewrites page.html in place (mirrors real ACP write_artifact).
		if req.NodeType == "visual" && !f.visualSkipProduces {
			body := f.nextStructuredBody(req.NodeID,
				"<!doctype html><html><body><h1>revised</h1></body></html>")
			_, _ = f.host.WriteArtifact(req.RunID, req.Token, req.NodeID, visualPageName, body, "html")
		}
	}
	msg := "已按标注就地修改。"
	if revErr != nil {
		msg = "就地修改失败: " + revErr.Error()
	}
	f.mu.Lock()
	usage := models.CloneTokenUsage(f.reviseUsage)
	f.mu.Unlock()
	return runtime.ReactTurn{Msg: msg, Done: false, Err: revErr, Usage: usage,
		Events: []models.AcpEvent{{Kind: "message", Text: "revise-in-place"}}}
}

func (f *fakeProvider) OfferCommitOnConfirm(ctx context.Context, req runtime.NodeReq) runtime.ReactTurn {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.retired[f.parkKey(req.RunID, req.NodeID)] {
		f.wrapUpAfterRetire = true
	}
	if f.reconcileCalls[req.NodeID] == 0 {
		f.wrapUpBeforeReconcile = true
	}
	if f.wrapUpCalls == nil {
		f.wrapUpCalls = map[string]int{}
	}
	f.wrapUpCalls[req.NodeID]++
	return runtime.ReactTurn{Msg: f.wrapUpMsg}
}

// ReconcileOnConfirm mirrors the real provider's confirm-time pair: it reports a
// narration for the transcript plus the AgentSummary the hidden summary turn
// produced, and (like the real reconcile) may rewrite the structured product.
func (f *fakeProvider) ReconcileOnConfirm(ctx context.Context, req runtime.NodeReq) runtime.ReactTurn {
	f.mu.Lock()
	if f.reconcileCalls == nil {
		f.reconcileCalls = map[string]int{}
	}
	f.reconcileCalls[req.NodeID]++
	msg, summary := f.reconcileMsg, f.reconcileSummary
	f.mu.Unlock()
	if msg == "" {
		msg = "已按聊天记录核对产物。"
	}
	return runtime.ReactTurn{Msg: msg, AgentSummary: summary,
		Events: []models.AcpEvent{{Kind: "message", Text: "confirm-reconcile"}}}
}

func (f *fakeProvider) HasLiveSession(runID, nodeID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.retired[f.parkKey(runID, nodeID)] {
		return false
	}
	return true // the fake always has a "parked" session until retired
}

func (f *fakeProvider) RetireSession(runID, nodeID string) {
	f.mu.Lock()
	if f.retired == nil {
		f.retired = map[string]bool{}
	}
	f.retired[f.parkKey(runID, nodeID)] = true
	f.mu.Unlock()
}

// CancelSessionTurn is a no-op for the fake (no live ACP); optional interface.
func (f *fakeProvider) CancelSessionTurn(runID, nodeID string) {}

func kindOf(name string) string {
	switch {
	case strings.HasSuffix(name, ".json"):
		return "json"
	case strings.HasSuffix(name, ".yaml"), strings.HasSuffix(name, ".yml"):
		return "yaml"
	default:
		return "markdown"
	}
}
