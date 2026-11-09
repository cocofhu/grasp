package main

import (
	"errors"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// This foundation does not yet install execution guards on every old worker.
// The complete lifecycle layer replaces this startup barrier with Controller.
func requireUninitializedFoundation(db *gorm.DB) error {
	var count int64
	if err := db.Model(&models.ExecutionDataset{}).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return errors.New("initialized execution dataset requires the complete execution-guarded server")
	}
	// Upgrade intentionally retains maintenance until explicit initialization.
	// Also recognize the durable format evidence if maintenance was separately
	// cleared: absence of a dataset is not evidence that old workers are safe.
	if db.Migrator().HasTable(&database.OfflineUpgradeReceipt{}) {
		var receipt struct{ ID string }
		result := db.Model(&database.OfflineUpgradeReceipt{}).Select("id").Limit(1).Find(&receipt)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 0 {
			return errors.New("offline-normalized history requires the complete execution-guarded server")
		}
	}
	// A partially completed importer can have published canonical ranges before
	// its final receipt. Inspect only the inline pointer, never object bodies.
	var message struct{ ID string }
	result := db.Table("solo_messages").Select("id").
		Where("event_log IS NOT NULL AND event_log NOT IN ?", []string{"", "null"}).Limit(1).Find(&message)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 0 {
		return errors.New("journal-backed history requires the complete execution-guarded server")
	}
	return nil
}
