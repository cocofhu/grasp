package services

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/tokenledger"
)

func TestTokenAccountingDistinguishesMissingZeroAndProvisionalReports(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "accounting.db"))
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorkspaceService(db)
	d := NewDashboardService(db, w)
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	entry := tokenledger.Entry{ID: "missing", At: now, Source: models.TokenLedgerSourceSolo}
	tokenledger.Record(db, entry)
	assertStatus := func(wantTokens *int64, incomplete bool) {
		t.Helper()
		status, err := d.PlatformStatus(context.Background(), PlatformStatusQuery{Timezone: "UTC", Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if (status.CumulativeTokens == nil) != (wantTokens == nil) || wantTokens != nil && *status.CumulativeTokens != *wantTokens || status.Accounting.Incomplete != incomplete {
			t.Fatalf("platform=%+v expected=%v incomplete=%v", status, wantTokens, incomplete)
		}
		global, err := w.GlobalTokenStats(context.Background(), GlobalTokenStatsQuery{Window: TokenStatsWindowAll, Timezone: "UTC", Now: now})
		if err != nil {
			t.Fatal(err)
		}
		if global.Accounting.Incomplete != incomplete {
			t.Fatalf("global=%+v", global.Accounting)
		}
	}
	assertStatus(nil, true)
	entry.Usage = &models.TokenUsage{}
	tokenledger.Record(db, entry)
	zero := int64(0)
	assertStatus(&zero, false)
	entry.ID = "provisional"
	entry.Usage = &models.TokenUsage{InputTokens: 7}
	entry.Partial = true
	tokenledger.Record(db, entry)
	seven := int64(7)
	assertStatus(&seven, true)
	entry.Usage = &models.TokenUsage{InputTokens: 19}
	entry.Partial = false
	tokenledger.Record(db, entry)
	nineteen := int64(19)
	assertStatus(&nineteen, false)
}

func TestPlatformStatusReturnsDatabaseError(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDashboardService(db, NewWorkspaceService(db)).PlatformStatus(context.Background(), PlatformStatusQuery{}); err == nil {
		t.Fatal("database failure must not become zero activity or missing token values")
	}
}

func TestLegacyAccountingRemainsVisibleWithoutClaimingVerification(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	seedLedger(t, db, models.TokenUsageEvent{Source: models.TokenLedgerSourceSolo, CreatedAt: now, InputTokens: 11})
	d := NewDashboardService(db, NewWorkspaceService(db))
	status, err := d.PlatformStatus(context.Background(), PlatformStatusQuery{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if status.CumulativeTokens == nil || *status.CumulativeTokens != 11 || !status.Accounting.LegacyUnverified || !status.Accounting.Incomplete {
		t.Fatalf("legacy usage was lost or declared verified: %+v", status)
	}
}
