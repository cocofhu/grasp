// Package ledgertest rebuilds the token ledger from StateRun / ChatMessage
// usage that tests seed directly, so stats tests can keep seeding node and
// message rows instead of driving the live ledger sinks.
package ledgertest

import (
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/tokenledger"
	"gorm.io/gorm"
)

// Sync replaces every ledger row with entries derived from the seeded
// StateRun / assistant ChatMessage usage. Safe to call repeatedly.
func Sync(db *gorm.DB) error {
	if err := db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&models.TokenUsageEvent{}).Error; err != nil {
		return err
	}
	var srs []models.StateRun
	if err := db.Where("usage IS NOT NULL OR usage_by_model IS NOT NULL").Find(&srs).Error; err != nil {
		return err
	}
	for _, sr := range srs {
		var run models.Run
		_ = db.Select("id", "started_at", "created_at").Where("id = ?", sr.RunID).Limit(1).Find(&run).Error
		at := run.StartedAt
		if sr.StartedAt != nil && !sr.StartedAt.IsZero() {
			at = *sr.StartedAt
		}
		if at.IsZero() {
			at = run.CreatedAt
		}
		if at.IsZero() {
			continue
		}
		tokenledger.Record(db, tokenledger.Entry{
			At: at, Source: models.TokenLedgerSourceWorkflow,
			Phase: models.TokenLedgerPhaseProduction, Status: tokenledger.StatusFromNode(sr.Status),
			RunID: sr.RunID, NodeID: sr.NodeID, NodeType: sr.NodeType,
			Usage: sr.Usage, ByModel: sr.UsageByModel,
		})
	}
	var msgs []models.ChatMessage
	if err := db.Where("role = ? AND (usage IS NOT NULL OR usage_by_model IS NOT NULL)", "assistant").Find(&msgs).Error; err != nil {
		return err
	}
	for _, m := range msgs {
		if m.CreatedAt.IsZero() {
			continue
		}
		tokenledger.Record(db, tokenledger.Entry{
			At: m.CreatedAt, Source: models.TokenLedgerSourcePM,
			Phase: models.TokenLedgerPhaseChat, Status: models.TokenLedgerStatusOK,
			ThreadID: m.ThreadID, Usage: m.Usage, ByModel: m.UsageByModel,
		})
	}
	return nil
}
