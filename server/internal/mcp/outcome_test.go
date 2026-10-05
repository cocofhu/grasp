package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

type memOutcomeStore struct {
	data map[string]map[string]string
}

func (m *memOutcomeStore) Save(runID, nodeID, name, kind, content string) (string, error) {
	if m.data == nil {
		m.data = map[string]map[string]string{}
	}
	if m.data[runID] == nil {
		m.data[runID] = map[string]string{}
	}
	m.data[runID][name] = content
	return "id-" + name, nil
}
func (m *memOutcomeStore) Get(runID, name string) (string, bool) {
	c, ok := m.data[runID][name]
	return c, ok
}
func (m *memOutcomeStore) List(runID string) []ArtifactInfo { return nil }
func (m *memOutcomeStore) Delete(runID, name string) error {
	if m.data != nil && m.data[runID] != nil {
		delete(m.data[runID], name)
	}
	return nil
}

type countingValidator struct {
	calls  int
	accept bool
	msg    string
}

func (c *countingValidator) Validate(ctx context.Context, in OutcomeValidateIn) (OutcomeValidateOut, error) {
	c.calls++
	return OutcomeValidateOut{Accept: c.accept, Message: c.msg}, nil
}

func TestChainedOutcomeValidatorDefaultThenRPC(t *testing.T) {
	rpc := &countingValidator{accept: true}
	chain := ChainedOutcomeValidator{Default: DefaultOutcomeValidator{}, RPC: rpc}

	// Bad status → Default rejects, RPC not called.
	out, err := chain.Validate(context.Background(), OutcomeValidateIn{
		Outcome: NodeOutcome{Status: "maybe"},
	})
	if err != nil || out.Accept {
		t.Fatalf("want default reject, got accept=%v err=%v", out.Accept, err)
	}
	if rpc.calls != 0 {
		t.Fatalf("RPC called on default failure: %d", rpc.calls)
	}

	// Good status → RPC called.
	out, err = chain.Validate(context.Background(), OutcomeValidateIn{
		Outcome: NodeOutcome{Status: OutcomeSuccess},
	})
	if err != nil || !out.Accept {
		t.Fatalf("want accept, got %+v err=%v", out, err)
	}
	if rpc.calls != 1 {
		t.Fatalf("RPC calls = %d, want 1", rpc.calls)
	}

	// Default-only (nil RPC).
	only := ChainedOutcomeValidator{Default: DefaultOutcomeValidator{}}
	out, err = only.Validate(context.Background(), OutcomeValidateIn{
		Outcome: NodeOutcome{Status: OutcomeFailed},
	})
	if err != nil || !out.Accept {
		t.Fatalf("failed status should be acceptable to mark: %+v err=%v", out, err)
	}
}

type failAuditStore struct {
	memOutcomeStore
}

func (f *failAuditStore) Save(runID, nodeID, name, kind, content string) (string, error) {
	if name == NodeOutcomeArtifactName {
		return "", errors.New("audit write blocked")
	}
	return f.memOutcomeStore.Save(runID, nodeID, name, kind, content)
}

func TestNodeCompleteMorePaths(t *testing.T) {
	h := NewHost(&memOutcomeStore{})
	tok := h.RegisterRun("r1")

	msg, isErr := h.runTool("r1", "bad-token", "node_complete", map[string]any{"status": "success"})
	if !isErr || !strings.Contains(msg, "node_complete failed:") {
		t.Fatalf("want auth failure, got %q err=%v", msg, isErr)
	}

	msg, isErr = h.runTool("r1", tok, "node_complete", map[string]any{"status": "success"})
	if !isErr || !strings.Contains(msg, "无活跃节点") {
		t.Fatalf("want no active node, got %q err=%v", msg, isErr)
	}

	h.SetActiveNode("r1", "n1", capsWriting(models.SchemaResearch))
	msg, isErr = h.runTool("r1", tok, "node_complete", map[string]any{
		"status": "failed",
		"error":  "boom",
	})
	if isErr || !strings.Contains(msg, "boom") {
		t.Fatalf("failed status: %q err=%v", msg, isErr)
	}
	h.ClearOutcome("r1", "n1")

	msg, isErr = h.runTool("r1", tok, "node_complete", map[string]any{"status": "success"})
	if isErr || !strings.Contains(msg, "success") {
		t.Fatalf("empty summary defaults to success: %q err=%v", msg, isErr)
	}

	h2 := NewHost(&failAuditStore{})
	tok2 := h2.RegisterRun("r2")
	h2.SetActiveNode("r2", "n2", capsWriting(models.SchemaResearch))
	msg, isErr = h2.runTool("r2", tok2, "node_complete", map[string]any{"status": "success", "summary": "audit"})
	if isErr || !strings.Contains(msg, "audit") {
		t.Fatalf("audit write failure should still succeed mark: %q err=%v", msg, isErr)
	}
}

func TestNodeCompleteTool(t *testing.T) {
	h := NewHost(&memOutcomeStore{})
	tok := h.RegisterRun("r1")
	h.SetActiveNode("r1", "n1", capsWriting(models.SchemaResearch))

	msg, isErr := h.runTool("r1", tok, "node_complete", map[string]any{
		"status":  "success",
		"summary": "done",
		"outputs": map[string]any{"mr_url": "http://mr/1"},
	})
	if isErr {
		t.Fatalf("node_complete error: %s", msg)
	}
	o, ok := h.PeekOutcome("r1", "n1")
	if !ok || o.Status != OutcomeSuccess || o.Outputs["mr_url"] != "http://mr/1" {
		t.Fatalf("peek = %+v ok=%v", o, ok)
	}
	taken, ok := h.TakeOutcome("r1", "n1")
	if !ok || taken.Summary != "done" {
		t.Fatalf("take = %+v ok=%v", taken, ok)
	}
	if _, ok := h.TakeOutcome("r1", "n1"); ok {
		t.Fatal("second take should miss")
	}

	// Invalid status rejected.
	msg, isErr = h.runTool("r1", tok, "node_complete", map[string]any{"status": "nope"})
	if !isErr || !strings.Contains(msg, "status") {
		t.Fatalf("want status error, got %q err=%v", msg, isErr)
	}
}

func TestClearOutcome(t *testing.T) {
	store := &memOutcomeStore{}
	h := NewHost(store)
	tok := h.RegisterRun("r1")
	h.SetActiveNode("r1", "n1", capsPlain)
	if msg, isErr := h.runTool("r1", tok, "node_complete", map[string]any{
		"status": "success", "summary": "stale",
	}); isErr {
		t.Fatalf("node_complete: %s", msg)
	}
	if !h.HasOutcome("r1", "n1") {
		t.Fatal("expected buffered outcome")
	}
	if _, ok := store.Get("r1", NodeOutcomeArtifactName); !ok {
		t.Fatal("expected audit artifact")
	}
	h.ClearOutcome("r1", "n1")
	if h.HasOutcome("r1", "n1") {
		t.Fatal("ClearOutcome should drop the mark")
	}
	if _, ok := store.Get("r1", NodeOutcomeArtifactName); ok {
		t.Fatal("ClearOutcome should delete audit artifact")
	}
	h.ClearOutcome("r1", "n1") // idempotent
}

func TestClassifyAndRestoreOutcomeArtifact(t *testing.T) {
	store := &memOutcomeStore{}
	h := NewHost(store)
	h.RegisterRun("r1")
	h.SetActiveNode("r1", "n1", capsPlain)
	_, _ = store.Save("r1", "n1", NodeOutcomeArtifactName, "json",
		OutcomeJSON(NodeOutcome{Status: OutcomeSuccess, Summary: "from art"}))
	if h.HasOutcome("r1", "n1") {
		t.Fatal("memory should be empty before restore")
	}
	if !h.RestoreOutcomeFromArtifact("r1", "n1") {
		t.Fatal("restore failed")
	}
	o, ok := h.PeekOutcome("r1", "n1")
	if !ok || o.Status != OutcomeSuccess || o.Summary != "from art" {
		t.Fatalf("restored=%v ok=%v", o, ok)
	}
	_, st := ClassifyOutcomeArtifact("{bad")
	if st != OutcomeArtifactCorrupt {
		t.Fatalf("corrupt state=%v", st)
	}
	_, st = ClassifyOutcomeArtifact(`{"status":"nope"}`)
	if st != OutcomeArtifactCorrupt {
		t.Fatalf("bad status state=%v", st)
	}
}

func TestSetRPCOutcomeValidator(t *testing.T) {
	h := NewHost(&memOutcomeStore{})
	rpc := &countingValidator{accept: false, msg: "biz reject"}
	h.SetRPCOutcomeValidator(rpc)

	out, err := h.ValidateOutcome(context.Background(), OutcomeValidateIn{
		Outcome: NodeOutcome{Status: OutcomeSuccess},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Accept || out.Message != "biz reject" {
		t.Fatalf("want rpc reject, got %+v", out)
	}
	if rpc.calls != 1 {
		t.Fatalf("rpc calls = %d", rpc.calls)
	}

	// Default failure still skips RPC.
	rpc.calls = 0
	out, err = h.ValidateOutcome(context.Background(), OutcomeValidateIn{
		Outcome: NodeOutcome{Status: "bad"},
	})
	if err != nil || out.Accept {
		t.Fatalf("want default reject: %+v", out)
	}
	if rpc.calls != 0 {
		t.Fatalf("rpc should not run after default fail, calls=%d", rpc.calls)
	}
}
