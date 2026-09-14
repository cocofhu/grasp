package mcp

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// TestResearchTestImplementTools exercises the set_/get_ tools not covered by
// the existing suite (research, test_result, implementation_result), driving
// parseResearch / parseTestResult / parseImplementationResult + structuredGet.
func TestResearchTestImplementTools(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	runID := "r"
	tok := h.RegisterRun(runID)

	h.SetActiveNode(runID, "res", "research")
	call(t, h, runID, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"set_research","arguments":{"summary":"调研","questions":[{"question":"Q1","answer":"A1"}],"findings":[{"title":"F1","detail":"d"}],"recommendation":"用A"}}}`)
	if _, ok := store.Get(runID, ResearchArtifactName); !ok {
		t.Fatal("research.json not written")
	}
	if _, isErr := toolText(t, call(t, h, runID, tok, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_research","arguments":{}}}`)); isErr {
		t.Fatal("get_research errored")
	}

	h.SetActiveNode(runID, "tst", "test")
	call(t, h, runID, tok, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"set_test_result","arguments":{"summary":"测试","passed":3,"failed":1,"skipped":0,"cases":[{"name":"c1","status":"passed"},{"name":"c2","status":"failed"}]}}}`)
	if _, ok := store.Get(runID, TestResultArtifactName); !ok {
		t.Fatal("test_result.json not written")
	}
	call(t, h, runID, tok, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_test_result","arguments":{}}}`)

	h.SetActiveNode(runID, "imp", "implement")
	call(t, h, runID, tok, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"set_implementation_result","arguments":{"summary":"实现","changes":["a.go"],"branch":"feat","tests":"ok"}}}`)
	if _, ok := store.Get(runID, ImplementationResultArtifactName); !ok {
		t.Fatal("implementation_result.json not written")
	}
	call(t, h, runID, tok, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_implementation_result","arguments":{}}}`)

	// get_* for a missing artifact returns an error result.
	h2 := NewHost(&memStore{})
	t2 := h2.RegisterRun("r2")
	if _, isErr := toolText(t, call(t, h2, "r2", t2, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"get_review","arguments":{}}}`)); !isErr {
		t.Error("get_review on empty store should error")
	}
}

// TestCallToolErrorBranches exercises the guard/error branches of callTool that
// the happy-path tests don't reach: auth, node-type gating, empty/invalid args,
// and the update_plan_status state machine.
func TestCallToolErrorBranches(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	runID := "r"
	tok := h.RegisterRun(runID)

	assertErr := func(body string) {
		t.Helper()
		if _, isErr := toolText(t, call(t, h, runID, tok, body)); !isErr {
			t.Fatalf("expected error result for %s", body)
		}
	}
	tc := func(id int, name, argsJSON string) string {
		return `{"jsonrpc":"2.0","id":` + itoa(id) + `,"method":"tools/call","params":{"name":"` + name + `","arguments":` + argsJSON + `}}`
	}

	// write_artifact without a name.
	assertErr(tc(1, "write_artifact", `{"content":"x"}`))
	// unknown tool.
	assertErr(tc(2, "does_not_exist", `{}`))

	// ask_question with a wrong token (tool-level authorize fails).
	if _, isErr := toolText(t, call(t, h, runID, "wrong-token", tc(3, "ask_question", `{"questions":[{"prompt":"q"}]}`))); !isErr {
		t.Fatal("ask_question with bad token should error")
	}
	// ask_question on a non-react node.
	h.SetActiveNode(runID, "n", "agent")
	assertErr(tc(4, "ask_question", `{"questions":[{"prompt":"q"}]}`))
	// ask_question on a react node but with empty questions.
	h.SetActiveNode(runID, "n", "react")
	assertErr(tc(5, "ask_question", `{"questions":[]}`))

	// set_plan on a non-plan node.
	h.SetActiveNode(runID, "n", "agent")
	assertErr(tc(6, "set_plan", `{"goals":[{"id":"g1","title":"G"}]}`))

	// update_plan_status before a plan exists (any node type).
	assertErr(tc(7, "update_plan_status", `{"id":"g1","status":"done"}`))
	// implement node from here on.
	h.SetActiveNode(runID, "n", "implement")
	// missing id.
	assertErr(tc(8, "update_plan_status", `{"status":"done"}`))
	// invalid status.
	assertErr(tc(9, "update_plan_status", `{"id":"g1","status":"bogus"}`))
	// no plan yet.
	assertErr(tc(10, "update_plan_status", `{"id":"g1","status":"done"}`))
	// get_plan with no plan.
	assertErr(tc(11, "get_plan", `{}`))

	// Write a plan (as a plan node) then update a non-existent item id.
	h.SetActiveNode(runID, "p", "plan")
	call(t, h, runID, tok, tc(12, "set_plan", `{"goals":[{"id":"g1","title":"目标","subgoals":[{"id":"s1","title":"子"}]}]}`))
	h.SetActiveNode(runID, "n", "implement")
	assertErr(tc(13, "update_plan_status", `{"id":"nope","status":"done"}`))
	// A valid update succeeds (subgoal ids are normalized to g<goal>.<sub>).
	if _, isErr := toolText(t, call(t, h, runID, tok, tc(14, "update_plan_status", `{"id":"g1.1","status":"done"}`))); isErr {
		t.Fatal("valid update_plan_status should succeed")
	}
	infos, err := h.ListArtifacts(runID, tok)
	if err != nil {
		t.Fatalf("list after update_plan_status: %v", err)
	}
	keptWriter := false
	for _, info := range infos {
		if info.Name != PlanArtifactName {
			continue
		}
		keptWriter = true
		if info.Node != "p" {
			t.Fatalf("update_plan_status must keep plan.json writer %q, got %q", "p", info.Node)
		}
	}
	if !keptWriter {
		t.Fatal("plan.json missing after update_plan_status")
	}
	// Any node may backfill plan status once a plan exists.
	h.SetActiveNode(runID, "n", "agent")
	if _, isErr := toolText(t, call(t, h, runID, tok, tc(16, "update_plan_status", `{"id":"g1.1","status":"done"}`))); isErr {
		t.Fatal("update_plan_status on agent node should succeed")
	}
	// Cleared active node (e.g. run finished) may still backfill.
	h.SetActiveNode(runID, "", "")
	if _, isErr := toolText(t, call(t, h, runID, tok, tc(17, "update_plan_status", `{"id":"g1.1","status":"done"}`))); isErr {
		t.Fatal("update_plan_status with no active node should succeed")
	}

	// structuredSet auth failure (wrong token) and wrong-node gating.
	h.SetActiveNode(runID, "n", "research")
	if _, isErr := toolText(t, call(t, h, runID, "wrong-token", tc(20, "set_research", `{"summary":"s","findings":[{"title":"f"}]}`))); !isErr {
		t.Fatal("set_research bad token should error")
	}
	h.SetActiveNode(runID, "n", "agent") // wrong node type for set_research
	assertErr(tc(21, "set_research", `{"summary":"s","findings":[{"title":"f"}]}`))
	// structuredSet parse error (empty summary) on the correct node.
	h.SetActiveNode(runID, "n", "research")
	assertErr(tc(22, "set_research", `{}`))
	// structuredGet auth failure.
	if _, isErr := toolText(t, call(t, h, runID, "wrong-token", tc(23, "get_research", `{}`))); !isErr {
		t.Fatal("get_research bad token should error")
	}

	// parseQuestions accepts bare-string and object options, fills ids, and
	// skips prompt-less entries.
	qs := parseQuestions([]any{
		map[string]any{"text": "问题一", "options": []any{"A", map[string]any{"id": "b", "label": "B"}}},
		map[string]any{"id": "x", "prompt": "问题二", "allowMultiple": true},
		map[string]any{"foo": "bar"}, // no prompt -> skipped
		"not-an-object",              // skipped
	})
	if len(qs) != 2 || qs[0].ID != "q1" || qs[1].ID != "x" || !qs[1].AllowMultiple {
		t.Fatalf("parseQuestions = %+v", qs)
	}
	if parseQuestions("not-a-list") != nil {
		t.Error("parseQuestions non-list -> nil")
	}

	// Single-select: at most one recommended kept (first wins).
	rq := parseQuestions([]any{
		map[string]any{"prompt": "选一个", "options": []any{
			map[string]any{"label": "A"},
			map[string]any{"label": "B", "recommended": true},
			map[string]any{"label": "C", "recommended": true},
		}},
	})
	if len(rq) != 1 || len(rq[0].Options) != 3 {
		t.Fatalf("recommended parse shape = %+v", rq)
	}
	if rq[0].Options[0].Recommended || !rq[0].Options[1].Recommended || rq[0].Options[2].Recommended {
		t.Fatalf("expected only the first recommended kept for single-select, got %+v", rq[0].Options)
	}

	// Multi-select: keep every recommended mark.
	mq := parseQuestions([]any{
		map[string]any{"prompt": "多选", "allowMultiple": true, "options": []any{
			map[string]any{"label": "A", "recommended": true},
			map[string]any{"label": "B"},
			map[string]any{"label": "C", "recommended": true},
		}},
	})
	if len(mq) != 1 || len(mq[0].Options) != 3 || !mq[0].AllowMultiple {
		t.Fatalf("multi recommended parse shape = %+v", mq)
	}
	if !mq[0].Options[0].Recommended || mq[0].Options[1].Recommended || !mq[0].Options[2].Recommended {
		t.Fatalf("expected all recommended kept for multi-select, got %+v", mq[0].Options)
	}

	// demoHtml is parsed and preserved; empty values are ignored.
	dh := parseQuestions([]any{
		map[string]any{"prompt": "UI?", "options": []any{
			map[string]any{"label": "A", "demoHtml": "<!doctype html><html></html>", "recommended": true},
			map[string]any{"label": "B", "demoHtml": ""},
			"纯文字",
		}},
	})
	if len(dh) != 1 || len(dh[0].Options) != 3 {
		t.Fatalf("demoHtml parse shape = %+v", dh)
	}
	if dh[0].Options[0].DemoHtml != "<!doctype html><html></html>" || !dh[0].Options[0].Recommended {
		t.Fatalf("demoHtml + recommended = %+v", dh[0].Options[0])
	}
	if dh[0].Options[1].DemoHtml != "" || dh[0].Options[2].DemoHtml != "" {
		t.Fatalf("empty demoHtml should be ignored, got %+v", dh[0].Options[1:])
	}

	// ServeRPC transport branches: parse error, unknown method, notification.
	if status, _ := h.ServeRPC(runID, tok, []byte(`not json`)); status != 400 {
		t.Errorf("parse error status = %d", status)
	}
	if _, resp := h.ServeRPC(runID, tok, []byte(`{"jsonrpc":"2.0","id":1,"method":"no_such_method"}`)); !strings.Contains(string(resp), "method not found") {
		t.Error("unknown method should report method not found")
	}
	if status, _ := h.ServeRPC(runID, tok, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); status != 202 {
		t.Errorf("notification status = %d", status)
	}
}

func itoa(i int) string {
	return strconv.Itoa(i)
}

// TestSetTestResultValidatesScreenshotArtifacts verifies the CLI-upload flow: a
// screenshot already in the store (as artifact-upload would leave it) is
// referenced by name in set_test_result, and the stored test_result.json keeps
// the artifact ref (no inline data). Missing artifact refs reject the whole write.
func TestSetTestResultValidatesScreenshotArtifacts(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	runID := "r"
	tok := h.RegisterRun(runID)
	h.SetActiveNode(runID, "tst", "test")

	// Seed via upload_image_artifact (artifact-upload CLI path); write_artifact
	// must not be used for images (see TestWriteArtifactKindValidation).
	if _, err := h.UploadImageArtifact(runID, tok, "tst", "shot-1.png", pngB64()); err != nil {
		t.Fatalf("seed screenshot: %v", err)
	}

	// Missing artifact must fail the entire write.
	failResp := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"set_test_result","arguments":{"summary":"s","cases":[{"name":"c1","status":"passed"}],"screenshots":[{"artifact":"shot-1.png","caption":"home"},{"artifact":"missing.png"}]}}}`)
	failResult := failResp["result"].(map[string]any)
	if failResult["isError"] != true {
		t.Fatalf("missing artifact should error, got: %v", failResp)
	}
	failText := failResult["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(failText, "missing.png") {
		t.Fatalf("error should name missing artifact, got: %s", failText)
	}
	if _, ok := store.Get(runID, TestResultArtifactName); ok {
		t.Fatal("test_result.json should not be written when validation fails")
	}

	// All artifacts present: stored JSON keeps artifact ref, no inline data.
	call(t, h, runID, tok, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"set_test_result","arguments":{"summary":"s","cases":[{"name":"c1","status":"passed"}],"screenshots":[{"artifact":"shot-1.png","caption":"home","mimeType":"image/png"}]}}}`)

	content, ok := store.Get(runID, TestResultArtifactName)
	if !ok {
		t.Fatal("test_result.json not written")
	}
	var doc struct {
		Screenshots []struct {
			Data     string `json:"data"`
			Artifact string `json:"artifact"`
			MimeType string `json:"mimeType"`
			Caption  string `json:"caption"`
		} `json:"screenshots"`
	}
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatalf("unmarshal stored test_result: %v (%s)", err, content)
	}
	if len(doc.Screenshots) != 1 {
		t.Fatalf("screenshots = %d, want 1: %+v", len(doc.Screenshots), doc.Screenshots)
	}
	s := doc.Screenshots[0]
	if s.Data != "" || s.Artifact != "shot-1.png" || s.Caption != "home" || s.MimeType != "image/png" {
		t.Errorf("want artifact-only with metadata preserved, got: %+v", s)
	}

	// Input with both data and artifact: data stripped, metadata kept.
	call(t, h, runID, tok, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"set_test_result","arguments":{"summary":"s2","cases":[{"name":"c1","status":"passed"}],"screenshots":[{"artifact":"shot-1.png","data":"SHOULD_NOT_STORE","caption":"with-data","mimeType":"image/webp"}]}}}`)
	content2, ok := store.Get(runID, TestResultArtifactName)
	if !ok {
		t.Fatal("test_result.json not rewritten")
	}
	if err := json.Unmarshal([]byte(content2), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(doc.Screenshots) != 1 {
		t.Fatalf("screenshots = %d, want 1", len(doc.Screenshots))
	}
	s2 := doc.Screenshots[0]
	if s2.Data != "" || s2.Artifact != "shot-1.png" || s2.Caption != "with-data" || s2.MimeType != "image/webp" {
		t.Errorf("data+artifact input should store ref+metadata only: %+v", s2)
	}
}

func TestHostAuthorizeBranches(t *testing.T) {
	h := NewHost(&memStore{})
	tok := h.RegisterRun("r")

	// Wrong token is rejected everywhere.
	if _, err := h.WriteArtifact("r", "bad", "n", "f.md", "x", "markdown"); err != ErrUnauthorized {
		t.Errorf("write unauth = %v", err)
	}
	if _, err := h.ReadArtifact("r", "bad", "f.md"); err != ErrUnauthorized {
		t.Errorf("read unauth = %v", err)
	}
	if _, err := h.ListArtifacts("r", "bad"); err != ErrUnauthorized {
		t.Errorf("list unauth = %v", err)
	}
	if _, err := h.PlanIncomplete("r", "bad"); err != ErrUnauthorized {
		t.Errorf("plan unauth = %v", err)
	}

	// Correct token: read of a missing artifact -> not found.
	if _, err := h.ReadArtifact("r", tok, "missing.md"); err == nil {
		t.Error("read missing should error")
	}
	// PlanIncomplete with no plan -> error.
	if _, err := h.PlanIncomplete("r", tok); err == nil {
		t.Error("no plan should error")
	}
	// Write then list.
	if _, err := h.WriteArtifact("r", tok, "n", "f.md", "hi", "markdown"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if infos, err := h.ListArtifacts("r", tok); err != nil || len(infos) != 1 {
		t.Fatalf("list = %v %v", infos, err)
	}

	// After UnregisterRun the token no longer authorizes.
	h.UnregisterRun("r")
	if _, err := h.ReadArtifact("r", tok, "f.md"); err != ErrUnauthorized {
		t.Errorf("post-unregister read = %v", err)
	}
}

func TestParsePlanEdgeCases(t *testing.T) {
	// Empty goals.
	if _, err := parsePlan(map[string]any{}); err == nil {
		t.Error("empty goals should error")
	}
	// Goal missing title.
	if _, err := parsePlan(map[string]any{"goals": []any{map[string]any{"detail": "d"}}}); err == nil {
		t.Error("goal without title should error")
	}
	// Subgoal missing title.
	if _, err := parsePlan(map[string]any{"goals": []any{map[string]any{"title": "G", "subgoals": []any{map[string]any{"detail": "d"}}}}}); err == nil {
		t.Error("subgoal without title should error")
	}
	// Third level not allowed.
	deep := map[string]any{"goals": []any{map[string]any{"title": "G", "subgoals": []any{
		map[string]any{"title": "S", "subgoals": []any{map[string]any{"title": "too deep"}}},
	}}}}
	if _, err := parsePlan(deep); err == nil {
		t.Error("three-level plan should error")
	}
	// Valid two-level plan normalizes ids.
	doc, err := parsePlan(map[string]any{"title": "P", "goals": []any{
		map[string]any{"title": "G1", "subgoals": []any{map[string]any{"title": "S1"}, map[string]any{"title": "S2"}}},
	}})
	if err != nil {
		t.Fatalf("valid plan: %v", err)
	}
	if doc.Goals[0].ID != "g1" || doc.Goals[0].Subgoals[1].ID != "g1.2" {
		t.Errorf("plan ids = %+v", doc.Goals)
	}
}

// TestRenderMarkdownAll calls every structured Render* (engine-facing) function
// with representative JSON so their formatting branches are covered.
func TestRenderMarkdownAll(t *testing.T) {
	checks := []struct {
		name   string
		render func(string) string
		json   string
		want   string
	}{
		{"clarified", RenderClarifiedRequirementMarkdown,
			`{"title":"T","summary":"s","background":"bg","goals":["g"],"in_scope":["in"],"out_of_scope":["out"],"functional_requirements":[{"id":"f1","title":"登录","detail":"d","priority":"must","acceptance_criteria":["ok"]}],"assumptions":["a"],"dependencies":["d"],"constraints":["c1"],"open_questions":["q1"]}`, "登录"},
		{"research", RenderResearchMarkdown,
			`{"summary":"s","questions":[{"question":"q1","answer":"a1"}],"findings":[{"title":"F","detail":"d"}],"recommendation":"r"}`, "F"},
		{"proposals", RenderProposalsMarkdown,
			`{"context":"ctx","proposals":[{"id":"p1","title":"A","pros":["good"],"cons":["bad"]},{"id":"p2","title":"B","recommended":true}]}`, "A"},
		{"proposal", RenderProposalMarkdown,
			`{"id":"p2","title":"B","summary":"chosen","pros":["x"],"cons":["y"],"status":"accepted"}`, "B"},
		{"test", RenderTestResultMarkdown,
			`{"summary":"s","passed":2,"failed":1,"skipped":0,"cases":[{"name":"c1","status":"passed"},{"name":"c2","status":"failed","detail":"boom"}]}`, "c2"},
		{"review", RenderReviewMarkdown,
			`{"summary":"s","verdict":"request_changes","findings":[{"title":"crit","severity":"critical","detail":"d","suggestion":"fix"}]}`, "crit"},
		{"impl", RenderImplementationResultMarkdown,
			`{"summary":"done","changes":["a.go","b.go"],"branch":"feat","tests":"ok","follow_ups":["later"]}`, "done"},
		{"plan", RenderPlanMarkdown,
			`{"title":"计划","goals":[{"id":"g1","title":"G1","status":"in_progress","subgoals":[{"id":"g1.1","title":"S1","status":"done"}]},{"id":"g2","title":"G2","status":"pending"}]}`, "G1"},
	}
	for _, c := range checks {
		got := c.render(c.json)
		if strings.TrimSpace(got) == "" {
			t.Errorf("%s: empty markdown", c.name)
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: markdown missing %q:\n%s", c.name, c.want, got)
		}
		// Malformed JSON must not panic.
		_ = c.render("{not json")
	}
}

// TestAuthorizeSandboxLifetimeFallback covers the persistence-backed fallback:
// once the in-memory registration is gone (run finished / server restart), the
// persisted token still authorizes for as long as the run has a live sandbox,
// and stops the moment the sandbox is gone.
func TestAuthorizeSandboxLifetimeFallback(t *testing.T) {
	h := NewHost(&memStore{})
	tok := h.RegisterRun("run")
	// Simulate run finished / restart: in-memory token dropped, no source yet.
	h.UnregisterRun("run")
	if h.AuthorizeRun("run", tok) {
		t.Fatal("without a token source a revoked token must not authorize")
	}
	// Wire a source reporting the persisted token and whether a sandbox lives.
	alive := true
	h.SetRunTokenSource(func(runID string) (string, bool, bool) {
		if runID != "run" {
			return "", false, false
		}
		return tok, alive, true
	})
	if !h.AuthorizeRun("run", tok) {
		t.Error("token should authorize while a sandbox is alive")
	}
	if h.AuthorizeRun("run", "wrong") {
		t.Error("wrong token must be rejected even with a live sandbox")
	}
	// The success above re-cached the token in memory; clear it so the next
	// check exercises the fallback with the sandbox now gone.
	h.UnregisterRun("run")
	alive = false
	if h.AuthorizeRun("run", tok) {
		t.Error("token must stop authorizing once the sandbox is gone")
	}
	if h.AuthorizeRun("other", tok) {
		t.Error("unknown run must not authorize")
	}
}

// TestActiveNodeSourceFallback covers the node-type gate fallback: when the
// in-memory SetActiveNode registration is gone (server restart / a replica that
// never executed the node), ActiveNode/ActiveNodeType resolve from the persisted
// source so node-scoped tools (e.g. set_preview) re-gate correctly instead of
// seeing "" and being wrongly rejected.
func TestActiveNodeSourceFallback(t *testing.T) {
	h := NewHost(&memStore{})

	// No in-memory state and no source: gate sees the unknown defaults.
	if got := h.ActiveNodeType("run"); got != "" {
		t.Fatalf("ActiveNodeType without state = %q, want \"\"", got)
	}
	if got := h.ActiveNode("run"); got != "mcp" {
		t.Fatalf("ActiveNode without state = %q, want \"mcp\"", got)
	}

	calls := 0
	h.SetActiveNodeSource(func(runID string) (string, string, bool) {
		if runID != "run" {
			return "", "", false
		}
		calls++
		return "app_preview_auz1", "app_preview", true
	})

	// Fallback resolves the current node + type from persistence.
	if got := h.ActiveNodeType("run"); got != "app_preview" {
		t.Fatalf("ActiveNodeType via fallback = %q, want app_preview", got)
	}
	if got := h.ActiveNode("run"); got != "app_preview_auz1" {
		t.Fatalf("ActiveNode via fallback = %q, want app_preview_auz1", got)
	}
	// The first resolution re-cached both fields, so the second lookup is served
	// from memory without hitting the source again.
	if calls != 1 {
		t.Fatalf("source consulted %d times, want 1 (result should be cached)", calls)
	}

	// A live in-memory registration is authoritative and never consults the source.
	h.SetActiveNode("run", "impl", "implement")
	if got := h.ActiveNodeType("run"); got != "implement" {
		t.Fatalf("ActiveNodeType after SetActiveNode = %q, want implement", got)
	}
	if calls != 1 {
		t.Fatalf("source consulted %d times after in-memory set, want 1", calls)
	}

	// Unknown run: fallback reports no active node → gate keeps unknown defaults.
	if got := h.ActiveNodeType("other"); got != "" {
		t.Fatalf("ActiveNodeType for unknown run = %q, want \"\"", got)
	}
}

func TestHostLifecycleAndAuth(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	tok := h.RegisterRun("run")

	if !h.AuthorizeRun("run", tok) {
		t.Error("AuthorizeRun should accept the issued token")
	}
	if h.AuthorizeRun("run", "wrong") {
		t.Error("AuthorizeRun must reject a wrong token")
	}

	// Write then read.
	if _, err := h.WriteArtifact("run", tok, "n", "a.md", "body", ""); err != nil {
		t.Fatalf("WriteArtifact: %v", err)
	}
	if c, err := h.ReadArtifact("run", tok, "a.md"); err != nil || c != "body" {
		t.Fatalf("ReadArtifact = %q, %v", c, err)
	}
	if _, err := h.ReadArtifact("run", tok, "missing.md"); err == nil {
		t.Error("ReadArtifact missing should error")
	}
	if infos, err := h.ListArtifacts("run", tok); err != nil || len(infos) != 1 {
		t.Fatalf("ListArtifacts = %v, %v", infos, err)
	}

	// PlanIncomplete: no plan -> error; bad json -> error.
	if _, err := h.PlanIncomplete("run", tok); err == nil {
		t.Error("PlanIncomplete with no plan should error")
	}
	h.WriteArtifact("run", tok, "n", PlanArtifactName, "{bad", "json")
	if _, err := h.PlanIncomplete("run", tok); err == nil {
		t.Error("PlanIncomplete with bad json should error")
	}

	// RestoreRun re-binds a token (no-op on empty).
	h.UnregisterRun("run")
	if h.AuthorizeRun("run", tok) {
		t.Error("token should be revoked after UnregisterRun")
	}
	h.RestoreRun("run", "")
	if h.AuthorizeRun("run", "") {
		t.Error("RestoreRun with empty token must be a no-op")
	}
	h.RestoreRun("run", tok)
	if !h.AuthorizeRun("run", tok) {
		t.Error("RestoreRun should re-bind the token")
	}

	// Unauthorized host methods.
	if _, err := h.WriteArtifact("run", "bad", "n", "x", "y", ""); err != ErrUnauthorized {
		t.Errorf("WriteArtifact bad token = %v", err)
	}
	if _, err := h.ListArtifacts("run", "bad"); err != ErrUnauthorized {
		t.Errorf("ListArtifacts bad token = %v", err)
	}
}

func TestServeRPCEdges(t *testing.T) {
	h := NewHost(&memStore{})
	tok := h.RegisterRun("r")

	// Parse error -> 400.
	if status, _ := h.ServeRPC("r", tok, []byte(`{not json`)); status != 400 {
		t.Errorf("parse error status = %d, want 400", status)
	}
	// ping.
	if status, _ := h.ServeRPC("r", tok, []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)); status != 200 {
		t.Error("ping should be 200")
	}
	// Unknown method with id -> JSON-RPC error.
	call := func(body string) map[string]any { return callRaw(t, h, "r", tok, body) }
	resp := call(`{"jsonrpc":"2.0","id":2,"method":"bogus"}`)
	if resp["error"] == nil {
		t.Error("unknown method should return a JSON-RPC error")
	}
	// Unknown notification (no id) -> 202.
	if status, body := h.ServeRPC("r", tok, []byte(`{"jsonrpc":"2.0","method":"bogus/notify"}`)); status != 202 || body != nil {
		t.Errorf("unknown notification = %d/%s", status, body)
	}
	// initialize echoes the client protocol version.
	init := call(`{"jsonrpc":"2.0","id":3,"method":"initialize","params":{"protocolVersion":"2025-01-01"}}`)
	if init["result"].(map[string]any)["protocolVersion"] != "2025-01-01" {
		t.Error("initialize should echo the client protocolVersion")
	}
	// unknown tool name.
	if _, isErr := toolText(t, call(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nope","arguments":{}}}`)); !isErr {
		t.Error("unknown tool should be an error result")
	}
	// write_artifact missing name.
	if _, isErr := toolText(t, call(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"write_artifact","arguments":{"content":"x"}}}`)); !isErr {
		t.Error("write_artifact without name should error")
	}
}

// TestApplyPlanStatusParentGoal covers updating a big goal that has subgoals by
// its own id (g1) — the case that previously returned "未找到计划项" and left the
// leaf items pending, wrongly failing implement nodes. done on the parent must
// cascade to its subgoals (satisfying the leaf-only completion contract) while
// in_progress on the parent must not regress already-done subgoals.
func TestApplyPlanStatusParentGoal(t *testing.T) {
	newDoc := func() planDoc {
		return planDoc{Goals: []planGoal{{
			ID: "g1", Title: "目标一", Status: planStatusPending,
			Subgoals: []planSub{
				{ID: "g1.1", Title: "安装依赖", Status: planStatusPending},
				{ID: "g1.2", Title: "写代码", Status: planStatusPending},
			},
		}}}
	}

	// done on the parent id cascades to every subgoal and completes the plan.
	doc := newDoc()
	if !applyPlanStatus(&doc, "g1", planStatusDone) {
		t.Fatal("update_plan_status(g1, done) should match the parent goal")
	}
	for _, s := range doc.Goals[0].Subgoals {
		if s.Status != planStatusDone {
			t.Fatalf("subgoal %s not cascaded to done: %s", s.ID, s.Status)
		}
	}
	if doc.Goals[0].Status != planStatusDone {
		t.Fatalf("parent goal should roll up to done, got %s", doc.Goals[0].Status)
	}
	if inc := planIncomplete(doc); len(inc) != 0 {
		t.Fatalf("plan should be complete, still incomplete: %v", inc)
	}

	// in_progress on the parent shows progress without regressing a done subgoal.
	doc = newDoc()
	applyPlanStatus(&doc, "g1.1", planStatusDone)
	if !applyPlanStatus(&doc, "g1", planStatusInProgress) {
		t.Fatal("update_plan_status(g1, in_progress) should match the parent goal")
	}
	if doc.Goals[0].Status != planStatusInProgress {
		t.Fatalf("parent goal status = %s, want in_progress", doc.Goals[0].Status)
	}
	if doc.Goals[0].Subgoals[0].Status != planStatusDone {
		t.Fatal("already-done subgoal must not be regressed by a parent in_progress update")
	}

	// A truly unknown id still reports no match.
	doc = newDoc()
	if applyPlanStatus(&doc, "nope", planStatusDone) {
		t.Fatal("update_plan_status(nope, done) should not match anything")
	}
}

// callRaw is like the existing call helper but does not fail on a JSON-RPC
// error response (so error branches can be asserted).
func callRaw(t *testing.T, h *Host, runID, token, body string) map[string]any {
	t.Helper()
	_, resp := h.ServeRPC(runID, token, []byte(body))
	var out map[string]any
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatalf("bad json: %v (%s)", err, resp)
	}
	return out
}
