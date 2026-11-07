package runtime

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/tokenledger"
)

func TestRunAgentAccountingIncludesTransportRetryAndReprompt(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "accounting.db"))
	if err != nil {
		t.Fatal(err)
	}
	p, _, _, _, _, req := setupProvider(t, func(attempt int) chatFunc {
		return func(turn int) turnAction {
			if attempt == 0 {
				return turnAction{dropConn: true}
			}
			if turn == 0 {
				return turnAction{narration: "working", stopReason: "end_turn", usage: &models.TokenUsage{InputTokens: 5}}
			}
			return turnAction{narration: "done", stopReason: "end_turn", usage: &models.TokenUsage{OutputTokens: 7}, produces: map[string]string{"report.md": "ok"}}
		}
	})
	p.opts.RecordUsage = func(req NodeReq, result *sandbox.ChatResult, err error) {
		tokenledger.Record(db, tokenledger.Entry{
			ID: result.OpID, Source: models.TokenLedgerSourceWorkflow,
			RunID: req.RunID, NodeID: req.NodeID, NodeType: req.NodeType,
			Status: result.UsageStatus(err), Partial: result.StopReason == "",
			Usage: result.Usage, ByModel: result.UsageByModel,
		})
	}
	result, err := p.RunAgent(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	// The engine's aggregate fallback must not insert the same live usage again.
	tokenledger.Record(db, tokenledger.Entry{Source: models.TokenLedgerSourceWorkflow, Usage: result.Usage, ByModel: result.UsageByModel})
	var reports []models.TokenUsageReport
	if err := db.Find(&reports).Error; err != nil {
		t.Fatal(err)
	}
	var total, unavailable int64
	for _, report := range reports {
		total += report.Total
		if !report.Reported && report.Partial && report.Status == models.TokenLedgerStatusFailed {
			unavailable++
		}
	}
	// This fake does not submit node_complete: after the 5-token reply the
	// runtime sends all three completion nudges, each consuming seven tokens.
	if len(reports) != 5 || total != 26 || unavailable != 1 {
		t.Fatalf("actual calls were lost or double counted: %+v", reports)
	}
}
