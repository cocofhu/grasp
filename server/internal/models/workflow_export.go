package models

import "time"

// ExportSchemaVersion is the only supported envelope schema version.
const ExportSchemaVersion = 1

// ExportEnvelope is the portable JSON shape for workflow import/export.
// It intentionally omits runtime/instance fields (id, status, version, runs, …).
type ExportEnvelope struct {
	Defaults      *WorkflowDefaults `json:"defaults,omitempty"`
	SchemaVersion int               `json:"schemaVersion"`
	ExportedAt    time.Time         `json:"exportedAt"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	NeedsRepo     bool              `json:"needsRepo"`
	Graph         Graph             `json:"graph"`
}
