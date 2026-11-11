package models

import "time"

// ExecutionDataset identifies the current SQL execution generation. Servers
// keep this binding fixed at startup; offline recovery rotates it under the
// maintenance lock while preserving historical identities and quarantines.
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

// ExecutionRecovery keeps an offline operation fenced even if its private
// workdir ticket is lost. MarkerJSON is canonical recovery metadata, not a
// chat payload; only the matching operation may finalize it under maintenance.
type ExecutionRecovery struct {
	ID           string `gorm:"primaryKey;size:36"`
	Protocol     string `gorm:"size:64"`
	MarkerJSON   string `gorm:"type:text"`
	State        string `gorm:"size:32;index"`
	TicketSHA256 string `gorm:"size:64"`
	CreatedAt    time.Time
	FinalizedAt  *time.Time
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
