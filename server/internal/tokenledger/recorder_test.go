package tokenledger_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/tokenledger"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRecorderSurvivesFailureRestartAndUncertainCommit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounting.db")
	db, err := database.OpenSQLiteTest(path)
	if err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(dir, "journal")
	r, err := tokenledger.Start(db, journal)
	if err != nil {
		t.Fatal(err)
	}
	var blocked atomic.Bool
	blocked.Store(true)
	if err := db.Callback().Create().Before("gorm:create").Register("accounting-outage", func(tx *gorm.DB) {
		if blocked.Load() && tx.Statement.Table == "token_usage_events" {
			tx.AddError(errors.New("database unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	tokenledger.Record(db, tokenledger.Entry{ID: "actual-turn", Source: "solo", Usage: &models.TokenUsage{InputTokens: 20, CacheReadTokens: 30}})
	tokenledger.Record(db, tokenledger.Entry{ID: "actual-turn", Source: "solo", Partial: true, Usage: &models.TokenUsage{InputTokens: 10}})
	if got := tokenledger.Pending(db); got != 1 {
		t.Fatalf("pending=%d want 1", got)
	}
	var receipts int64
	if err := db.Model(&models.TokenUsageReport{}).Count(&receipts).Error; err != nil || receipts != 0 {
		t.Fatalf("failed transaction kept receipt: %d err=%v", receipts, err)
	}
	files, err := filepath.Glob(filepath.Join(journal, "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	oldSQL, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := oldSQL.Close(); err != nil {
		t.Fatal(err)
	}
	// A new process only has the database and durable journal, no old memory.
	db2, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := tokenledger.Start(db2, journal)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	assertLedgerTotal(t, db2, 1, 50)
	if got := tokenledger.Pending(db2); got != 0 {
		t.Fatalf("pending after restart=%d", got)
	}
	// Simulate a successful SQL commit followed by a crash before unlink.
	if err := os.WriteFile(files[0], raw, 0o600); err != nil {
		t.Fatal(err)
	}
	r2.Flush()
	tokenledger.Record(db2, tokenledger.Entry{ID: "actual-turn", Source: "solo", Usage: &models.TokenUsage{InputTokens: 20, CacheReadTokens: 30}})
	assertLedgerTotal(t, db2, 1, 50)
	if err := os.WriteFile(filepath.Join(journal, ".report-interrupted"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	r2.Flush()
	assertLedgerTotal(t, db2, 1, 50)
}

func assertLedgerTotal(t *testing.T, db *gorm.DB, count int, total int64) {
	t.Helper()
	var rows []models.TokenUsageEvent
	if err := db.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != count || sumRows(rows) != total {
		t.Fatalf("rows=%+v want count=%d total=%d", rows, count, total)
	}
}

func TestRecordConcurrentReplayAndIncompleteUpgrade(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "receipts.db"))
	if err != nil {
		t.Fatal(err)
	}
	entry := tokenledger.Entry{ID: "one-op", Source: "workflow", Partial: true}
	tokenledger.Record(db, entry)
	assertLedgerTotal(t, db, 0, 0)
	entry.Usage = &models.TokenUsage{InputTokens: 10}
	tokenledger.Record(db, entry)
	assertLedgerTotal(t, db, 1, 10)
	entry.Partial = false
	entry.Usage = &models.TokenUsage{InputTokens: 30, OutputTokens: 7}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { tokenledger.Record(db, entry) })
	}
	wg.Wait()
	assertLedgerTotal(t, db, 1, 37)
	// A stale provisional report must never overwrite the completed one.
	tokenledger.Record(db, tokenledger.Entry{ID: "one-op", Source: "workflow", Partial: true, Usage: &models.TokenUsage{InputTokens: 10}})
	assertLedgerTotal(t, db, 1, 37)
	var receipts []models.TokenUsageReport
	if err := db.Find(&receipts).Error; err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 1 || !receipts[0].Reported || receipts[0].Partial {
		t.Fatalf("receipts=%+v", receipts)
	}
}

func TestIdempotencyDoesNotDependOnDriverRowsAffected(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "matched.db"))
	if err != nil {
		t.Fatal(err)
	}
	// MySQL clientFoundRows can report a matched duplicate as one row. The
	// durable receipt, rather than this driver-specific count, must dedupe it.
	if err := db.Callback().Create().After("gorm:create").Register("matched-rows", func(tx *gorm.DB) {
		if tx.Statement.Table == "token_usage_reports" {
			tx.RowsAffected = 1
		}
	}); err != nil {
		t.Fatal(err)
	}
	entry := tokenledger.Entry{ID: "same-operation", Source: "solo", Usage: &models.TokenUsage{InputTokens: 12}}
	tokenledger.Record(db, entry)
	tokenledger.Record(db, entry)
	assertLedgerTotal(t, db, 1, 12)
}
