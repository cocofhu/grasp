package database

import (
	"strings"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

// retiredColumns belong to removed features: the workspace assistant, gate
// auto-invoke and notification policies. AutoMigrate does not drop them;
// startup does, and a second pass is a no-op.
var retiredColumns = map[string][]string{
	"workspaces": {
		"pm_leader_enabled", "pm_leader_agent", "pm_enabled_mcps", "pm_gate_auto_var", "pm_gate_auto_prompt",
		"notify_policy",
	},
	"workflow_defs": {"notify_policy"},
	"sandboxes":     {"recovery_mcp_names"},
}

// retiredTables held the workspace assistant, external MCP, IM channels,
// scheduled tasks, agent memory and run notification receipts.
var retiredTables = []string{
	"external_mcp_settings", "workspace_mcp_api_keys",
	"channel_configs", "chat_threads", "chat_messages", "chat_turn_drafts", "memory_items",
	"agent_cron_jobs", "agent_cron_runs", "notify_delivery_receipts",
}

// retiredValues renames stored enum values of the removed assistant to their
// current meaning.
var retiredValues = []struct {
	model    any
	column   string
	from, to string
}{
	{&models.AuditEvent{}, "caller_kind", "pm", models.CallerKindMember},
	{&models.Run{}, "trigger", "pm_mcp", models.TriggerAPI},
	{&models.WorkflowVersion{}, "source", "pm", models.VersionSourceSave},
}

// retiredLedgerSources are token ledger sources of the removed assistant and
// agent chat turns.
var retiredLedgerSources = []string{"pm", "chat"}

// retireRemovedFeatures removes the storage left by removed features. Missing
// tables and columns are success.
func retireRemovedFeatures(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	for _, v := range retiredValues {
		if err := db.Model(v.model).Where(map[string]any{v.column: v.from}).UpdateColumn(v.column, v.to).Error; err != nil {
			return err
		}
	}
	for _, m := range []any{&models.TokenUsageEvent{}, &models.TokenUsageReport{}} {
		if err := db.Where("source IN ?", retiredLedgerSources).Delete(m).Error; err != nil {
			return err
		}
	}
	for table, columns := range retiredColumns {
		if err := dropRetiredColumns(db, table, columns); err != nil {
			return err
		}
	}
	tables := make([]any, len(retiredTables))
	for i, name := range retiredTables {
		tables[i] = name
	}
	return db.Migrator().DropTable(tables...)
}

func dropRetiredColumns(db *gorm.DB, table string, columns []string) error {
	if !db.Migrator().HasTable(table) {
		return nil
	}
	types, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, col := range types {
		present[strings.ToLower(col.Name())] = true
	}
	for _, name := range columns {
		if !present[name] {
			continue
		}
		if err := db.Exec("ALTER TABLE " + table + " DROP COLUMN " + name).Error; err != nil {
			return err
		}
	}
	return nil
}
