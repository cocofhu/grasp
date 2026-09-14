package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// memStore is a minimal in-memory Store for testing the MCP dispatcher.
type memStore struct {
	data map[string]string // runID|name -> content
	kind map[string]string // runID|name -> kind
	node map[string]string // runID|name -> nodeID
	n    int
}

func (m *memStore) Save(runID, nodeID, name, kind, content string) (string, error) {
	if m.data == nil {
		m.data = map[string]string{}
	}
	if m.kind == nil {
		m.kind = map[string]string{}
	}
	if m.node == nil {
		m.node = map[string]string{}
	}
	key := runID + "|" + name
	m.data[key] = content
	m.kind[key] = kind
	m.node[key] = nodeID
	m.n++
	return "art-test", nil
}

func (m *memStore) kindOf(runID, name string) string {
	if m.kind == nil {
		return ""
	}
	return m.kind[runID+"|"+name]
}
func (m *memStore) Get(runID, name string) (string, bool) {
	c, ok := m.data[runID+"|"+name]
	return c, ok
}
func (m *memStore) List(runID string) []ArtifactInfo {
	var out []ArtifactInfo
	for k, v := range m.data {
		if strings.HasPrefix(k, runID+"|") {
			out = append(out, ArtifactInfo{
				Name: strings.TrimPrefix(k, runID+"|"),
				Node: m.node[k],
				Size: len(v),
			})
		}
	}
	return out
}

func call(t *testing.T, h *Host, runID, token, body string) map[string]any {
	t.Helper()
	status, resp := h.ServeRPC(runID, token, []byte(body))
	if status != 200 || resp == nil {
		t.Fatalf("unexpected status=%d resp=%s for %s", status, resp, body)
	}
	var out map[string]any
	if err := json.Unmarshal(resp, &out); err != nil {
		t.Fatalf("bad json: %v (%s)", err, resp)
	}
	return out
}

func TestMCPDispatcher(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	runID := "run-x"
	tok := h.RegisterRun(runID)

	// initialize
	init := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	res, _ := init["result"].(map[string]any)
	if res == nil || res["protocolVersion"] != mcpProtocolVersion {
		t.Fatalf("initialize result missing protocolVersion: %v", init)
	}

	// tools/list: 7 core + 2 history + 12 structured + set_preview + set_artifact_preview.
	list := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools, _ := list["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 24 {
		t.Fatalf("expected 24 tools, got %d", len(tools))
	}

	// tools/call write_artifact
	wr := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"write_artifact","arguments":{"name":"design.md","content":"hello"}}}`)
	r := wr["result"].(map[string]any)
	if r["isError"] == true {
		t.Fatalf("write_artifact reported error: %v", r)
	}
	if c, _ := store.Get(runID, "design.md"); c != "hello" {
		t.Fatalf("artifact not stored, got %q", c)
	}

	// read_artifact returns content
	rd := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"read_artifact","arguments":{"name":"design.md"}}}`)
	content := rd["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]
	if content != "hello" {
		t.Fatalf("read_artifact got %v", content)
	}

	// notifications/initialized → 202 no body
	if status, resp := h.ServeRPC(runID, tok, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); status != 202 || resp != nil {
		t.Fatalf("notification should be 202/no-body, got %d %s", status, resp)
	}

	// wrong token is unauthorized at the tool layer
	bad := call(t, h, runID, "wrong", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"list_artifacts","arguments":{}}}`)
	if bad["result"].(map[string]any)["isError"] != true {
		t.Fatalf("expected isError for bad token, got %v", bad)
	}

	// ask_question is clarify-only: rejected on a non-react active node.
	h.SetActiveNode(runID, "n1", "agent")
	aqBad := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"ask_question","arguments":{"questions":[{"prompt":"选哪个?","options":["A","B"]}]}}}`)
	if aqBad["result"].(map[string]any)["isError"] != true {
		t.Fatalf("ask_question should be rejected on non-react node, got %v", aqBad)
	}

	// On a react node it records the pending questions for the engine to drain.
	h.SetActiveNode(runID, "clarify", "react")
	aqOK := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"ask_question","arguments":{"questions":[{"prompt":"选哪个?","options":[{"label":"A"},{"label":"B"}],"allowMultiple":true}]}}}`)
	if aqOK["result"].(map[string]any)["isError"] == true {
		t.Fatalf("ask_question failed on react node: %v", aqOK)
	}
	qs := h.TakePendingQuestions(runID, "clarify")
	if len(qs) != 1 || len(qs[0].Options) != 2 || !qs[0].AllowMultiple || qs[0].ID == "" || qs[0].Options[0].ID == "" {
		t.Fatalf("pending question not recorded as expected: %+v", qs)
	}
	// Draining is destructive.
	if len(h.TakePendingQuestions(runID, "clarify")) != 0 {
		t.Fatalf("pending questions should be cleared after take")
	}
}

func toolText(t *testing.T, resp map[string]any) (string, bool) {
	t.Helper()
	r := resp["result"].(map[string]any)
	txt := r["content"].([]any)[0].(map[string]any)["text"].(string)
	isErr, _ := r["isError"].(bool)
	return txt, isErr
}

func TestPlanTools(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	runID := "run-plan"
	tok := h.RegisterRun(runID)

	// set_plan is plan-only: rejected on a non-plan node.
	h.SetActiveNode(runID, "impl", "implement")
	bad := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"set_plan","arguments":{"goals":[{"title":"G1"}]}}}`)
	if _, isErr := toolText(t, bad); !isErr {
		t.Fatalf("set_plan should be rejected on non-plan node")
	}

	// Three-level plan is rejected (subgoals may not nest).
	h.SetActiveNode(runID, "plan", "plan")
	deep := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"set_plan","arguments":{"goals":[{"title":"G1","subgoals":[{"title":"S1","subgoals":[{"title":"X"}]}]}]}}}`)
	if _, isErr := toolText(t, deep); !isErr {
		t.Fatalf("three-level plan should be rejected")
	}

	// Valid two-level plan on a plan node.
	ok := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"set_plan","arguments":{"goals":[{"title":"G1","subgoals":[{"title":"S1"},{"title":"S2"}]},{"title":"G2"}]}}}`)
	if _, isErr := toolText(t, ok); isErr {
		t.Fatalf("valid set_plan failed: %v", ok)
	}
	// Incomplete right after set_plan: 2 subgoals (g1.1,g1.2) + standalone g2.
	inc, err := h.PlanIncomplete(runID, tok)
	if err != nil || len(inc) != 3 {
		t.Fatalf("expected 3 incomplete items, got %d (err=%v)", len(inc), err)
	}

	// update_plan_status works from any node once a plan exists (including plan node).
	stOk := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"update_plan_status","arguments":{"id":"g1.1","status":"done"}}}`)
	if _, isErr := toolText(t, stOk); isErr {
		t.Fatalf("update_plan_status should succeed on plan node: %v", stOk)
	}

	// Move to the implement node and complete remaining leaves.
	h.SetActiveNode(runID, "impl", "implement")
	// Unknown id errors.
	unk := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"update_plan_status","arguments":{"id":"nope","status":"done"}}}`)
	if _, isErr := toolText(t, unk); !isErr {
		t.Fatalf("update_plan_status with unknown id should error")
	}
	// Invalid status errors.
	invalid := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"update_plan_status","arguments":{"id":"g1.1","status":"weird"}}}`)
	if _, isErr := toolText(t, invalid); !isErr {
		t.Fatalf("invalid status should error")
	}
	for _, id := range []string{"g1.2", "g2"} {
		r := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"update_plan_status","arguments":{"id":"`+id+`","status":"done"}}}`)
		if _, isErr := toolText(t, r); isErr {
			t.Fatalf("update_plan_status(%s) failed: %v", id, r)
		}
	}
	if inc, err := h.PlanIncomplete(runID, tok); err != nil || len(inc) != 0 {
		t.Fatalf("plan should be complete, got %d incomplete (err=%v)", len(inc), err)
	}

	// get_plan returns the stored plan; parent goal g1 rolled up to done.
	gp := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"get_plan","arguments":{}}}`)
	txt, isErr := toolText(t, gp)
	if isErr {
		t.Fatalf("get_plan failed: %v", gp)
	}
	var doc planDoc
	if err := json.Unmarshal([]byte(txt), &doc); err != nil {
		t.Fatalf("get_plan not valid json: %v", err)
	}
	if len(doc.Goals) != 2 || doc.Goals[0].Status != planStatusDone {
		t.Fatalf("plan rollup wrong: %+v", doc.Goals)
	}
}

// TestReviewPhaseTools covers the post-run ReAct review phase relaxations:
// ask_question opens up to any node while the run is in review, and the
// structured set_*/get_* tools stay authorized so the producer can rewrite its
// product in place across multiple review turns.
func TestReviewPhaseTools(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	runID := "run-review"
	tok := h.RegisterRun(runID)

	// A proposal node finished its automated run and is now in the review phase.
	h.SetActiveNode(runID, "prop", "proposal")
	h.SetActiveReview(runID, true)
	if !h.InReviewPhase(runID) {
		t.Fatalf("run should be marked in review phase")
	}

	// ask_question is normally clarify-only, but a review-phase node may raise
	// follow-up choices even though it is not a react node.
	aq := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask_question","arguments":{"questions":[{"prompt":"方案 A 还是 B?","options":[{"label":"A"},{"label":"B"}]}]}}}`)
	if _, isErr := toolText(t, aq); isErr {
		t.Fatalf("ask_question should be allowed during review, got %v", aq)
	}
	if qs := h.TakePendingQuestions(runID, "prop"); len(qs) != 1 {
		t.Fatalf("review ask_question should record one question, got %d", len(qs))
	}

	// set_proposals stays authorized so the producer rewrites proposals.json.
	rw := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"set_proposals","arguments":{"context":"复审重写","proposals":[{"title":"A2"},{"title":"B2","recommended":true}]}}}`)
	if _, isErr := toolText(t, rw); isErr {
		t.Fatalf("set_proposals should stay authorized during review: %v", rw)
	}
	if pc, ok := store.Get(runID, ProposalsArtifactName); !ok || !strings.Contains(pc, "B2") {
		t.Fatalf("proposals not rewritten during review: %q", pc)
	}

	// Leaving the review phase re-arms the clarify-only guard on ask_question.
	h.SetActiveReview(runID, false)
	if h.InReviewPhase(runID) {
		t.Fatalf("review phase should be cleared")
	}
	aqBad := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ask_question","arguments":{"questions":[{"prompt":"x","options":[{"label":"A"},{"label":"B"}]}]}}}`)
	if _, isErr := toolText(t, aqBad); !isErr {
		t.Fatalf("ask_question should be rejected once review ends on a non-react node")
	}
}

func TestStructuredTools(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	runID := "run-struct"
	tok := h.RegisterRun(runID)

	// set_clarified_requirement is react-only: rejected on a non-react node.
	h.SetActiveNode(runID, "n", "agent")
	bad := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"set_clarified_requirement","arguments":{"summary":"s","functional_requirements":[{"title":"f"}]}}}`)
	if _, isErr := toolText(t, bad); !isErr {
		t.Fatalf("set_clarified_requirement should be rejected off a react node")
	}

	// On a react node it writes clarified_requirement.json and assigns ids.
	h.SetActiveNode(runID, "clarify", "react")
	okArgs := `{
		"title":"登录需求","summary":"用户可用邮箱验证码登录","background":"需要安全登录入口",
		"goals":["完成邮箱验证码登录"],"in_scope":["邮箱验证码登录"],"out_of_scope":["第三方 OAuth"],
		"functional_requirements":[{"title":"验证码登录","detail":"用户输入邮箱与验证码完成登录","acceptance_criteria":["5 分钟有效"]}],
		"assumptions":["用户已有邮箱"],"dependencies":["邮件发送服务可用"],"constraints":"仅邮箱"
	}`
	ok := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"set_clarified_requirement","arguments":`+okArgs+`}}`)
	if _, isErr := toolText(t, ok); isErr {
		t.Fatalf("set_clarified_requirement failed: %v", ok)
	}
	content, found := store.Get(runID, ClarifiedRequirementArtifactName)
	if !found {
		t.Fatalf("clarified_requirement.json not stored")
	}
	var cr map[string]any
	if err := json.Unmarshal([]byte(content), &cr); err != nil {
		t.Fatalf("clarified requirement not valid json: %v", err)
	}
	fr, _ := cr["functional_requirements"].([]any)
	if len(fr) != 1 {
		t.Fatalf("functional requirement count: %+v", fr)
	}
	f0, _ := fr[0].(map[string]any)
	if f0["id"] != "f1" {
		t.Fatalf("functional requirement id not assigned: %+v", f0)
	}
	constraints, _ := cr["constraints"].([]any)
	if len(constraints) != 1 || constraints[0] != "仅邮箱" {
		t.Fatalf("constraints not coerced: %+v", constraints)
	}

	// Thin / incomplete payload is a validation error.
	missing := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"set_clarified_requirement","arguments":{"summary":"only","functional_requirements":[{"title":"f"}]}}}`)
	if _, isErr := toolText(t, missing); !isErr {
		t.Fatalf("thin clarified requirement should error")
	}

	// set_proposals on a proposal node, then SelectProposal picks the recommended.
	h.SetActiveNode(runID, "prop", "proposal")
	pr := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"set_proposals","arguments":{"context":"选型","proposals":[{"title":"A"},{"title":"B","recommended":true}]}}}`)
	if _, isErr := toolText(t, pr); isErr {
		t.Fatalf("set_proposals failed: %v", pr)
	}
	pc, _ := store.Get(runID, ProposalsArtifactName)
	if choices := ProposalChoices(pc); len(choices) != 2 || choices[0].ID != "p1" {
		t.Fatalf("proposal choices wrong: %+v", choices)
	}
	final, id, okSel := SelectProposal(pc, "")
	if !okSel || id != "p2" {
		t.Fatalf("auto-select should pick recommended p2, got %q (ok=%v)", id, okSel)
	}
	if !strings.Contains(final, `"status": "accepted"`) {
		t.Fatalf("final proposal missing accepted status: %s", final)
	}

	// set_review normalizes verdict and sorts findings by severity.
	h.SetActiveNode(runID, "rev", "review")
	rv := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"set_review","arguments":{"summary":"ok","verdict":"request_changes","findings":[{"title":"low1","severity":"low"},{"title":"crit1","severity":"critical"}]}}}`)
	if _, isErr := toolText(t, rv); isErr {
		t.Fatalf("set_review failed: %v", rv)
	}
	rc, _ := store.Get(runID, ReviewArtifactName)
	if ReviewVerdict(rc) != "request_changes" {
		t.Fatalf("review verdict wrong: %s", rc)
	}
	var rvDoc map[string]any
	_ = json.Unmarshal([]byte(rc), &rvDoc)
	findings, _ := rvDoc["findings"].([]any)
	if len(findings) != 2 {
		t.Fatalf("review findings count: %+v", findings)
	}
	revF0, _ := findings[0].(map[string]any)
	if revF0["severity"] != "critical" {
		t.Fatalf("review findings not severity-sorted: %+v", findings)
	}
}

func TestSetArtifactPreview(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	runID := "run-preview"
	tok := h.RegisterRun(runID)

	h.SetActiveNode(runID, "n", "agent")
	if _, isErr := toolText(t, call(t, h, runID, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"set_artifact_preview","arguments":{"name":"page.html"}}}`)); !isErr {
		t.Fatal("set_artifact_preview on non-react node should fail")
	}

	h.SetActiveNode(runID, "c1", "react")
	if _, isErr := toolText(t, call(t, h, runID, tok, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"set_artifact_preview","arguments":{}}}`)); !isErr {
		t.Fatal("set_artifact_preview without name should fail")
	}
	if _, isErr := toolText(t, call(t, h, runID, tok, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"set_artifact_preview","arguments":{"name":"missing.html"}}}`)); !isErr {
		t.Fatal("set_artifact_preview for missing artifact should fail")
	}

	call(t, h, runID, tok, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"write_artifact","arguments":{"name":"page.html","content":"<html/>","kind":"html"}}}`)
	got := ""
	h.SetArtifactPreviewHook(func(rid, nodeID, name string) error {
		got = rid + "|" + nodeID + "|" + name
		return nil
	})
	txt, isErr := toolText(t, call(t, h, runID, tok, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"set_artifact_preview","arguments":{"name":"page.html"}}}`))
	if isErr {
		t.Fatalf("set_artifact_preview should succeed: %s", txt)
	}
	if got != "run-preview|c1|page.html" {
		t.Fatalf("hook got %q", got)
	}

	if _, isErr := toolText(t, call(t, h, runID, tok, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"set_artifact_preview","arguments":{"name":"   "}}}`)); !isErr {
		t.Fatal("set_artifact_preview with blank name should fail")
	}

	h.SetArtifactPreviewHook(func(rid, nodeID, name string) error {
		return fmt.Errorf("pin failed")
	})
	if txt, isErr := toolText(t, call(t, h, runID, tok, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"set_artifact_preview","arguments":{"name":"page.html"}}}`)); !isErr {
		t.Fatal("set_artifact_preview should surface hook errors")
	} else if !strings.Contains(txt, "pin failed") {
		t.Fatalf("hook error text=%q", txt)
	}
}

func TestApproveNodePreDevTools(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	runID := "run-approve"
	tok := h.RegisterRun(runID)
	h.SetActiveNode(runID, "pre", "approve")

	aq := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask_question","arguments":{"questions":[{"prompt":"选哪个?","options":[{"label":"A"},{"label":"B"}]}]}}}`)
	if _, isErr := toolText(t, aq); isErr {
		t.Fatalf("ask_question should work on approve: %v", aq)
	}

	okArgs := `{
		"title":"登录需求","summary":"用户可用邮箱验证码登录","background":"需要安全登录入口",
		"goals":["完成邮箱验证码登录"],"in_scope":["邮箱验证码登录"],"out_of_scope":["第三方 OAuth"],
		"functional_requirements":[{"title":"验证码登录","detail":"用户输入邮箱与验证码完成登录","acceptance_criteria":["5 分钟有效"]}],
		"assumptions":["用户已有邮箱"],"dependencies":["邮件发送服务可用"],"constraints":"仅邮箱"
	}`
	cr := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"set_clarified_requirement","arguments":`+okArgs+`}}`)
	if _, isErr := toolText(t, cr); isErr {
		t.Fatalf("set_clarified_requirement on approve: %v", cr)
	}
	plan := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"set_plan","arguments":{"goals":[{"title":"G1","subgoals":[{"title":"S1"}]}]}}}`)
	if _, isErr := toolText(t, plan); isErr {
		t.Fatalf("set_plan on approve: %v", plan)
	}
	res := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"set_research","arguments":{"summary":"调研概述","findings":[{"title":"F1","detail":"d"}]}}}`)
	if _, isErr := toolText(t, res); isErr {
		t.Fatalf("set_research on approve: %v", res)
	}
	pr := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"set_proposals","arguments":{"context":"选型","proposals":[{"title":"A"}]}}}`)
	if _, isErr := toolText(t, pr); isErr {
		t.Fatalf("set_proposals on approve: %v", pr)
	}
	if _, err := h.WriteArtifact(runID, tok, "pre", "page.html", "<!doctype html><html></html>", "html"); err != nil {
		t.Fatal(err)
	}
	prev := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"set_artifact_preview","arguments":{"name":"page.html"}}}`)
	if _, isErr := toolText(t, prev); isErr {
		t.Fatalf("set_artifact_preview on approve: %v", prev)
	}

	impl := call(t, h, runID, tok, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"set_implementation_result","arguments":{"summary":"x"}}}`)
	if _, isErr := toolText(t, impl); !isErr {
		t.Fatal("set_implementation_result must stay blocked on approve")
	}
}

func TestUploadImageArtifactSniffsContent(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	runID := "sniff-run"
	tok := h.RegisterRun(runID)
	h.SetActiveNode(runID, "tst", "test")

	ok, err := h.UploadImageArtifact(runID, tok, "tst", "shot.png", pngB64())
	if err != nil || ok == "" {
		t.Fatalf("png header should pass: id=%q err=%v", ok, err)
	}
	if got := store.kindOf(runID, "shot.png"); got != "image" {
		t.Fatalf("kind=%q", got)
	}

	_, err = h.UploadImageArtifact(runID, tok, "tst", "page-as-shot.png", htmlB64())
	if err == nil || !strings.Contains(err.Error(), "write_artifact") {
		t.Fatalf("html upload should be rejected with write_artifact hint, got %v", err)
	}

	_, err = h.UploadImageArtifact(runID, tok, "tst", "bad.png", "not-base64!!!")
	if err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("invalid base64 should be rejected, got %v", err)
	}
}
