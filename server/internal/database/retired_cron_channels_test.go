package database

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestDropRetiredCronChannelsIsIdempotent(t *testing.T) {
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
		"CREATE TABLE agent_cron_jobs (id TEXT PRIMARY KEY)",
		"CREATE TABLE agent_cron_runs (id TEXT PRIMARY KEY)",
		"CREATE TABLE channel_configs (id TEXT PRIMARY KEY)",
		"CREATE TABLE notify_delivery_receipts (id INTEGER PRIMARY KEY)",
		"ALTER TABLE workspaces ADD COLUMN notify_policy TEXT",
		"ALTER TABLE workflow_defs ADD COLUMN notify_policy TEXT",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatal(err)
		}
	}
	threads := []models.ChatThread{
		{ID: "web", UserID: "alice", Kind: models.ChatThreadKindUser},
		{ID: "cron", UserID: "cron:agent", Kind: "cron"},
		{ID: "qq", UserID: "qq:group:g1", Kind: models.ChatThreadKindUser},
		{ID: "dingtalk", UserID: "dingtalk:c2c:u1"},
	}
	if err := db.Create(&threads).Error; err != nil {
		t.Fatal(err)
	}
	for _, th := range threads {
		if err := db.Create(&models.ChatMessage{ID: "m-" + th.ID, ThreadID: th.ID, Role: "user"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&models.ChatTurnDraft{ID: "d-" + th.ID, ThreadID: th.ID}).Error; err != nil {
			t.Fatal(err)
		}
	}

	saved := models.Sandbox{RecoveryMCPNames: []string{"memory-store", "task-scheduler", "context-store"}}
	if err := db.Create(&saved).Error; err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if err := dropRetiredCronChannels(db); err != nil {
			t.Fatal(err)
		}
		for _, table := range retiredCronChannelTables {
			if db.Migrator().HasTable(table) {
				t.Fatalf("%s still exists", table)
			}
		}
		for table, column := range retiredNotifyPolicyColumns {
			if db.Migrator().HasColumn(table, column) {
				t.Fatalf("%s.%s still exists", table, column)
			}
		}
		var ids []string
		db.Model(&models.ChatThread{}).Pluck("id", &ids)
		if len(ids) != 1 || ids[0] != "web" {
			t.Fatalf("threads = %v", ids)
		}
		var msgs, drafts int64
		db.Model(&models.ChatMessage{}).Count(&msgs)
		db.Model(&models.ChatTurnDraft{}).Count(&drafts)
		if msgs != 1 || drafts != 1 {
			t.Fatalf("messages=%d drafts=%d", msgs, drafts)
		}
		var got models.Sandbox
		if err := db.First(&got, saved.ID).Error; err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got.RecoveryMCPNames, []string{"memory-store", "context-store"}) {
			t.Fatalf("recovery MCP names = %v", got.RecoveryMCPNames)
		}
	}
	if err := dropRetiredCronChannels(nil); err != nil {
		t.Fatal(err)
	}
}
