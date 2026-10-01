package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/tokenledger"
	"gorm.io/gorm"
)

// syncTokenLedger re-imports StateRun / ChatMessage usage seeded directly by a
// test into the token ledger that every stats surface reads.
func syncTokenLedger(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := tokenledger.Backfill(db); err != nil {
		t.Fatal(err)
	}
}
