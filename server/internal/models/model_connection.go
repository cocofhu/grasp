package models

import (
	"github.com/cocofhu/grasp/internal/modelrouting"
	"time"
)

// ModelConnection stores a non-secret service connection and credential reference.
type ModelConnection modelrouting.Connection

// ModelConfiguration persists a coding tool and concrete model binding.
type ModelConfiguration modelrouting.Configuration

// ModelRoutingMigration records a legacy mapping even after the user deletes
// its connection, preventing a later list/read from resurrecting that choice.
type ModelRoutingMigration struct {
	CredentialID string `gorm:"primaryKey"`
	ConnectionID string
	CreatedAt    time.Time
}
