package models

import (
	"encoding/json"
	"time"
)

// WorkflowEventSource freezes one result source in the Run transition's SQL
// transaction. Metadata includes immutable public fields and, for legacy SQL
// bodies, their original bytes. Reference columns have explicit ownership;
// none of these fields performs object I/O through an ORM serializer.
type WorkflowEventSource struct {
	ID        string `gorm:"primaryKey;size:36"`
	EventID   string `gorm:"size:36;uniqueIndex:idx_workflow_event_source,priority:1;index"`
	Position  int    `gorm:"uniqueIndex:idx_workflow_event_source,priority:2"`
	Kind      string
	Metadata  json.RawMessage `gorm:"type:longtext;serializer:json"`
	TextRef   string          `gorm:"payload:text"`
	JSONRef   string          `gorm:"payload:json"`
	BytesRef  string          `gorm:"payload:bytes"`
	BinaryRef string          `gorm:"payload:binary"`
	ImageRef  string          `gorm:"payload:image"`
	CreatedAt time.Time
}
