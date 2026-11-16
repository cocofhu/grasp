package database

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestRetireRemovedFeaturesRewritesValuesAndDropsStorage(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "retired.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	for _, stmt := range []string{
		"ALTER TABLE workspaces ADD COLUMN pm_leader_agent text",
		"ALTER TABLE workspaces ADD COLUMN notify_policy text",
		"ALTER TABLE workflow_defs ADD COLUMN notify_policy text",
		"ALTER TABLE sandboxes ADD COLUMN recovery_mcp_names text",
		"CREATE TABLE channel_configs (id text PRIMARY KEY, agent_name text)",
		"CREATE UNIQUE INDEX udx_channel_configs_agent_name ON channel_configs (agent_name)",
		"CREATE TABLE chat_messages (id text PRIMARY KEY, citations text)",
		"CREATE TABLE agent_cron_jobs (id text PRIMARY KEY)",
		"CREATE TABLE notify_delivery_receipts (id integer PRIMARY KEY)",
		"CREATE TABLE external_mcp_settings (id text PRIMARY KEY, enabled numeric)",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	old := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for i, source := range []string{"pm", "chat", models.TokenLedgerSourceWorkflow} {
		if err := db.Create(&models.TokenUsageEvent{ID: uint(i + 1), CreatedAt: old, Source: source, Status: "ok"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.TokenUsageReport{ID: source, CreatedAt: old, Source: source}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&models.AuditEvent{ID: "au", CallerKind: "pm", CreatedAt: old}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Run{ID: "run", Trigger: "pm_mcp", CreatedAt: old}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.Run{ID: "manual", Trigger: models.TriggerManual, CreatedAt: old}).Error; err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := retireRemovedFeatures(db); err != nil {
			t.Fatal(err)
		}
	}

	var events []models.TokenUsageEvent
	if err := db.Find(&events).Error; err != nil || len(events) != 1 || events[0].Source != models.TokenLedgerSourceWorkflow {
		t.Fatalf("ledger events=%+v err=%v", events, err)
	}
	var reports []models.TokenUsageReport
	if err := db.Find(&reports).Error; err != nil || len(reports) != 1 || reports[0].Source != models.TokenLedgerSourceWorkflow {
		t.Fatalf("ledger reports=%+v err=%v", reports, err)
	}
	var au models.AuditEvent
	if err := db.First(&au, "id = ?", "au").Error; err != nil || au.CallerKind != models.CallerKindMember {
		t.Fatalf("caller kind=%q err=%v", au.CallerKind, err)
	}
	var runs []models.Run
	if err := db.Order("id").Find(&runs).Error; err != nil {
		t.Fatal(err)
	}
	if runs[0].Trigger != models.TriggerManual || runs[1].Trigger != models.TriggerAPI {
		t.Fatalf("runs=%+v", runs)
	}
	for table, cols := range retiredColumns {
		for _, col := range cols {
			if db.Migrator().HasColumn(table, col) {
				t.Fatalf("%s.%s still present", table, col)
			}
		}
	}
	for _, table := range retiredTables {
		if db.Migrator().HasTable(table) {
			t.Fatalf("table %s still present", table)
		}
	}
	if err := db.Create(&models.Workspace{ID: "ws-after", Name: "after"}).Error; err != nil {
		t.Fatalf("workspace insert after drop: %v", err)
	}
	if err := retireRemovedFeatures(nil); err != nil {
		t.Fatal(err)
	}
}
