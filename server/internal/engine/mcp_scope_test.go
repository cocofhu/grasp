package engine

import (
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/mcp/mcptest"
	"github.com/cocofhu/grasp/internal/models"
)

// grantRunScope returns a token the host accepts for runID: the run row's
// persisted token when the run exists, else an in-memory test scope.
func grantRunScope(t *testing.T, eng *Engine, runID string) string {
	t.Helper()
	var run models.Run
	if err := eng.db.Limit(1).Find(&run, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	if run.ID == "" {
		return mcptest.Grant(eng.host, runID)
	}
	tok := mcp.NewToken()
	if err := eng.db.Model(&models.Run{}).Where("id = ?", runID).UpdateColumn("mcp_token", tok).Error; err != nil {
		t.Fatal(err)
	}
	return tok
}

// The workflow run's token follows the run status: valid while the run can
// execute, rejected once terminal, valid again after a resume. No restore or
// revoke call is involved, so a restart or another replica sees the same.
func TestRunScopeFollowsRunStatus(t *testing.T) {
	eng, db := setupEngine(t)
	const tok = "persisted-token"
	if err := db.Create(&models.Run{ID: "scoped", WorkflowID: "w", WorkflowName: "w", Status: "queued", McpToken: tok}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		status string
		want   bool
	}{
		{"queued", true}, {"running", true}, {"waiting_human", true},
		{"completed", false}, {"failed", false}, {"cancelled", false},
		{"running", true},
	} {
		db.Model(&models.Run{}).Where("id = ?", "scoped").Update("status", tc.status)
		if got := eng.host.AuthorizeRun("scoped", tok); got != tc.want {
			t.Errorf("status %s: authorized=%v want %v", tc.status, got, tc.want)
		}
	}
	if eng.host.AuthorizeRun("scoped", "wrong") {
		t.Error("wrong token authorized")
	}
	if eng.host.AuthorizeRun("missing", tok) {
		t.Error("unknown run authorized")
	}
}

// node_complete enforces the handoff from the run graph even when nothing in
// memory recorded the node's downstream (restart / another replica).
func TestNodeCompleteHandoffFromRunGraph(t *testing.T) {
	eng, db := setupEngine(t)
	g := models.Graph{
		Nodes: []models.Node{{ID: "impl", Type: "agent", Caps: capsPlain}, {ID: "test", Type: "agent", Label: "测试评审", Caps: capsPlain}, {ID: "gate", Type: "human_gate"}},
		Edges: []models.Edge{{ID: "e1", Source: "impl", Target: "test"}},
	}
	if err := db.Create(&models.Run{ID: "ho", WorkflowID: "w", WorkflowName: "w", Status: "running", Graph: g, McpToken: "tok"}).Error; err != nil {
		t.Fatal(err)
	}
	if got := eng.host.ActiveDownstream("ho", "impl"); len(got) != 1 || got[0].NodeID != "test" {
		t.Fatalf("downstream = %+v", got)
	}
	for _, node := range []string{"test", "gate", "absent"} {
		if got := eng.host.ActiveDownstream("ho", node); got != nil {
			t.Errorf("%s downstream = %+v, want none", node, got)
		}
	}
	if got := eng.host.ActiveDownstream("missing", "impl"); got != nil {
		t.Errorf("unknown run downstream = %+v", got)
	}
	eng.host.SetActiveNode("ho", "impl", capsPlain)
	_, resp := eng.host.ServeRPC("ho", "tok", []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"node_complete","arguments":{"status":"success"}}}`))
	if !strings.Contains(string(resp), "handoff") {
		t.Fatalf("node_complete without handoff accepted: %s", resp)
	}
}

// finish releases the run's in-memory MCP state only after the terminal
// status is persisted, so the release is never refused or premature.
func TestFinishReleasesRunMCPState(t *testing.T) {
	eng, db := setupEngine(t)
	if err := db.Create(&models.Run{ID: "fin", WorkflowID: "w", WorkflowName: "w", Status: "running", McpToken: "tok"}).Error; err != nil {
		t.Fatal(err)
	}
	eng.host.SetActiveNode("fin", "n", capsPlain)
	eng.finish("fin", "completed")
	if got := eng.host.ActiveNode("fin"); got != "mcp" {
		t.Fatalf("active node after finish = %q", got)
	}
	if eng.host.AuthorizeRun("fin", "tok") {
		t.Fatal("finished run still authorized")
	}
}
