package models

import "time"

// ExecutionDataset binds SQL to an independently held, externally active
// lineage. This row is comparison metadata, never the execution authority.
type ExecutionDataset struct {
	ID               string `gorm:"primaryKey;size:32"`
	NamespaceID      string `gorm:"size:36"`
	DatasetID        string `gorm:"size:36"`
	Lineage          string `gorm:"size:36"`
	ControlRevision  uint64
	OperationID      string `gorm:"size:36"`
	SourceDatasetID  string `gorm:"size:36"`
	SourceLineage    string `gorm:"size:128"`
	BackupSHA256     string `gorm:"size:64"`
	QuarantineCount  int64
	QuarantineSHA256 string `gorm:"size:64"`
	BoundAt          time.Time
}

// ExecutionQuarantine preserves review requirements across SQL restoration.
// Releasing one is a source-specific human operation; merely changing status or
// restarting the server must not replay an already dispatched unknown task.
type ExecutionQuarantine struct {
	ID              string `gorm:"primaryKey;size:64"`
	DatasetID       string `gorm:"size:36;index:idx_execution_review,priority:1"`
	OperationID     string `gorm:"size:36"`
	EntityKind      string `gorm:"size:32;index:idx_execution_entity,priority:1"`
	EntityID        string `gorm:"size:191;index:idx_execution_entity,priority:2"`
	PriorState      string `gorm:"size:64"`
	SourceDatasetID string `gorm:"size:36"`
	SourceLineage   string `gorm:"size:128"`
	ReviewState     string `gorm:"size:32;index:idx_execution_review,priority:2"`
	CreatedAt       time.Time
}
