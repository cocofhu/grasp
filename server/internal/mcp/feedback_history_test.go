package mcp

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

// The rollback entries were always in the trace; the reader simply ignored
// them, leaving a re-run node blind to the fact that it was sent back.
func TestRunHistorySurfacesRollbackToTargetNode(t *testing.T) {
	fh := twoGateRun()
	fh.run.Trace = []models.TraceEntry{
		{NodeID: "gate2", Event: "rollback", To: "code", Kind: models.EdgeRollback,
			Detail: "attempt=2 携带 [last_error] 回滚到 checkpoint"},
	}
	h := NewHost(&memStore{})
	h.SetHistoryProvider(fh)

	code, _ := h.RunHistory("r", "code", false, false)
	if !strings.Contains(code, "回退") || !strings.Contains(code, "attempt=2") {
		t.Fatalf("the rolled-back node must see why it is running again:\n%s", code)
	}
	design, _ := h.RunHistory("r", "design", false, false)
	if strings.Contains(design, "attempt=2") {
		t.Fatalf("rollback belongs to its target node only:\n%s", design)
	}
	// A rollback is a machine decision, not human feedback.
	onlyFb, _ := h.RunHistory("r", "code", false, true)
	if strings.Contains(onlyFb, "attempt=2") {
		t.Fatalf("only_feedback must exclude rollbacks:\n%s", onlyFb)
	}
	if !strings.Contains(onlyFb, "补充测试") {
		t.Fatalf("only_feedback must keep the gate decision:\n%s", onlyFb)
	}
	if strings.Contains(onlyFb, "feedback.") || strings.Contains(onlyFb, "feedback_index.json") {
		t.Fatalf("history must not cite feedback ledger files:\n%s", onlyFb)
	}
}

func TestFoldHistoryLinesKeepsHeadAndTail(t *testing.T) {
	line := strings.Repeat("x", 500)
	lines := make([]string, 80)
	for i := range lines {
		lines[i] = fmt.Sprintf("%02d-%s", i, line)
	}
	got := foldHistoryLines(lines)
	if len(got) >= len(lines) {
		t.Fatalf("oversized overview must fold, got %d lines", len(got))
	}
	if got[0] != lines[0] {
		t.Fatal("the earliest constraint must survive")
	}
	if got[len(got)-1] != lines[len(lines)-1] {
		t.Fatal("the newest instruction must survive")
	}
	marker := 0
	for _, l := range got {
		if strings.Contains(l, "省略") {
			marker++
		}
	}
	if marker != 1 {
		t.Fatalf("exactly one fold marker expected, got %d", marker)
	}
	small := []string{"a", "b", "c"}
	if len(foldHistoryLines(small)) != 3 {
		t.Fatal("within budget must not fold")
	}
}

func TestListAndReadTreatFeedbackNamesAsAbsent(t *testing.T) {
	store := &memStore{}
	h := NewHost(store)
	runID := "r"
	tok := h.RegisterRun(runID)
	for _, name := range []string{"research.json", FeedbackIndexArtifactName, "feedback.review.design.i1.json", "plan.json"} {
		if _, err := store.Save(runID, "design", name, "json", `{"body":"`+name+`"}`); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := h.ListArtifacts(runID, tok)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, a := range listed {
		names[a.Name] = true
		if a.Note != "" {
			t.Fatalf("listing must not synthesize a ledger note: %+v", a)
		}
	}
	if !names["research.json"] || !names["plan.json"] {
		t.Fatalf("formal products missing: %+v", listed)
	}
	if names[FeedbackIndexArtifactName] || names["feedback.review.design.i1.json"] {
		t.Fatalf("feedback names must be omitted: %+v", listed)
	}
	if _, err := h.ReadArtifact(runID, tok, FeedbackIndexArtifactName); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("index read err=%v", err)
	}
	if _, err := h.ReadArtifact(runID, tok, "feedback.review.design.i1.json"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("round read err=%v", err)
	}
	missing, err := h.ReadArtifact(runID, tok, "no-such.json")
	ledger, ledgerErr := h.ReadArtifact(runID, tok, "feedback.gate.g1.i1r1.json")
	if missing != ledger || err == nil || ledgerErr == nil || err.Error() != ledgerErr.Error() {
		t.Fatalf("ledger read must match a missing artifact: %q/%v vs %q/%v", missing, err, ledger, ledgerErr)
	}
	body, err := h.ReadArtifact(runID, tok, "research.json")
	if err != nil || !strings.Contains(body, "research.json") {
		t.Fatalf("formal read: %q %v", body, err)
	}
}
