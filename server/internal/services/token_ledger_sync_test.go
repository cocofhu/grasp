package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/tokenledger/ledgertest"
	"gorm.io/gorm"
)

// syncTokenLedger rebuilds the token ledger from StateRun / ChatMessage usage
// seeded directly by a test.
func syncTokenLedger(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := ledgertest.Sync(db); err != nil {
		t.Fatal(err)
	}
}
