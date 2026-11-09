package database

import (
	"context"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/storageguard"
	"gorm.io/gorm"
)

// OfflineUpgradeReceipt records normalization of one stopped database. It is
// not execution authority, permission to remove history readers, or a GC root.
// In particular LegacyEvents counts history that still needs its legacy reader.
type OfflineUpgradeReceipt struct {
	ID                   string    `gorm:"primaryKey;size:36" json:"id"`
	Protocol             string    `gorm:"size:64" json:"protocol"`
	SourceVersion        string    `gorm:"size:32" json:"sourceVersion"`
	SourceDatabaseSHA256 string    `gorm:"size:64" json:"sourceDatabaseSHA256"`
	ConfigurationSHA256  string    `gorm:"size:64" json:"configurationSHA256"`
	ReferenceSHA256      string    `gorm:"size:64" json:"referenceSHA256"`
	Cells                int64     `json:"cells"`
	Payloads             int64     `json:"payloads"`
	Attachments          int64     `json:"attachments"`
	LegacyPending        int64     `json:"legacyPending"`
	LegacyStarted        int64     `json:"legacyStarted"`
	LegacyEvents         int64     `json:"legacyEvents"`
	HistoryProtocol      string    `gorm:"size:64" json:"historyProtocol"`
	HistoryMessages      int64     `json:"historyMessages"`
	HistoryEvents        int64     `json:"historyEvents"`
	HistoryTruncated     int64     `json:"historyPossiblyTruncated"`
	HistoryEmptyMessages int64     `json:"historyEmptyMessages"`
	HistorySHA256        string    `gorm:"size:64" json:"historySHA256"`
	CompletedAt          time.Time `json:"completedAt"`
}

func (OfflineUpgradeReceipt) TableName() string { return "offline_upgrade_receipts" }

// UpgradeOfflineSchema reuses normal startup migrations without registering a
// server or constructing any workers. The caller must first preserve the
// original SQL and retain the same maintenance owner throughout the operation.
// Neither success nor failure releases maintenance.
func UpgradeOfflineSchema(ctx context.Context, db *gorm.DB, owner string) error {
	if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return errors.New("offline schema upgrade requires an outer connection")
	}
	if err := storageguard.Check(ctx, db, owner); err != nil {
		return err
	}
	if !db.Migrator().HasTable("runs") || !db.Migrator().HasTable("state_runs") {
		return errors.New("offline upgrade requires an existing complete core schema")
	}
	if _, err := finalize(db.WithContext(ctx)); err != nil {
		return err
	}
	return storageguard.Check(ctx, db, owner)
}
