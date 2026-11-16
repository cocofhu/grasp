package database

import (
	"strings"

	"gorm.io/gorm"
)

// retiredCronChannelTables held scheduled tasks, IM channel bindings and the
// Run→IM notification receipts.
var retiredCronChannelTables = []string{
	"agent_cron_runs",
	"agent_cron_jobs",
	"channel_configs",
	"notify_delivery_receipts",
}

// retiredNotifyPolicyColumns stored the Run→IM notification policies.
var retiredNotifyPolicyColumns = map[string]string{
	"workspaces":    "notify_policy",
	"workflow_defs": "notify_policy",
}

// retiredThreadOwnerPrefixes are the synthetic owners of scheduled-task and
// IM channel conversations. No user can sign in as them.
var retiredThreadOwnerPrefixes = []string{"cron:", "qq:", "wecom:", "feishu:", "dingtalk:"}

// dropRetiredCronChannels removes every table, column and conversation that
// belonged to scheduled tasks, IM channels and Run notifications. Missing
// tables and columns are success, so a second call is a no-op.
func dropRetiredCronChannels(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	for _, table := range retiredCronChannelTables {
		if err := db.Migrator().DropTable(table); err != nil {
			return err
		}
	}
	for table, column := range retiredNotifyPolicyColumns {
		if err := dropColumnIfPresent(db, table, column); err != nil {
			return err
		}
	}
	return deleteRetiredThreads(db)
}

func dropColumnIfPresent(db *gorm.DB, table, column string) error {
	if !db.Migrator().HasTable(table) {
		return nil
	}
	cols, err := db.Migrator().ColumnTypes(table)
	if err != nil {
		return err
	}
	for _, col := range cols {
		if strings.EqualFold(col.Name(), column) {
			return db.Exec("ALTER TABLE " + table + " DROP COLUMN " + column).Error
		}
	}
	return nil
}

func deleteRetiredThreads(db *gorm.DB) error {
	if !db.Migrator().HasTable("chat_threads") {
		return nil
	}
	q := db.Table("chat_threads").Where("kind = ?", "cron")
	for _, prefix := range retiredThreadOwnerPrefixes {
		q = q.Or("user_id LIKE ?", prefix+"%")
	}
	var ids []string
	if err := q.Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, table := range []string{"chat_turn_drafts", "chat_messages"} {
			if !tx.Migrator().HasTable(table) {
				continue
			}
			if err := tx.Exec("DELETE FROM "+table+" WHERE thread_id IN ?", ids).Error; err != nil {
				return err
			}
		}
		return tx.Exec("DELETE FROM chat_threads WHERE id IN ?", ids).Error
	})
}
