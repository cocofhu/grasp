package services

import (
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/tokenledger"
	"gorm.io/gorm"
)

// recordTurnUsage ledgers one ACP turn of a platform sandbox chat (Solo,
// Studio, preview visitors). The operation id keeps replays and Codex login
// retries to one report each; a turn without one is still recorded once.
// Failed and stopped turns consumed tokens too.
func recordTurnUsage(db *gorm.DB, e tokenledger.Entry, result *sandbox.ChatResult, err error) {
	if result == nil {
		return
	}
	e.ID, e.At = result.OpID, result.EndedAt
	// Only an identified operation can later be replaced by its final report.
	e.Partial = result.OpID != "" && (result.StopReason == "" || result.UsagePartial)
	e.Status = result.UsageStatus(err)
	e.Usage, e.ByModel = result.Usage, result.UsageByModel
	tokenledger.Record(db, e)
	if result.Usage != nil {
		result.Usage.LedgerRecorded = true
	}
}

// sandboxChatUsage is the ledger owner of a chat in a platform sandbox. Usage
// is keyed by run/thread ids and name snapshots, never by live rows, so it
// survives deleting the run, workflow or Solo session.
func sandboxChatUsage(row *models.Sandbox) tokenledger.Entry {
	e := tokenledger.Entry{Source: models.TokenLedgerSourceStudio, Phase: models.TokenLedgerPhaseChat, ThreadID: row.ThreadID, SandboxID: row.ID}
	if row.Purpose == SandboxPurposeSolo {
		e.Source = models.TokenLedgerSourceSolo
	}
	return e
}

// previewGuestUsage attributes an isolated preview visitor's turn to the owner
// of the shared preview: the Solo session, or the workflow node like the
// node's own visitor lanes.
func previewGuestUsage(row *models.Sandbox, runID, nodeID string) tokenledger.Entry {
	if row.Purpose == SandboxPurposeSolo {
		return sandboxChatUsage(row)
	}
	return tokenledger.Entry{
		Source: models.TokenLedgerSourceWorkflow, Phase: models.TokenLedgerPhaseInteractive,
		RunID: runID, NodeID: nodeID, SandboxID: row.ID,
	}
}
