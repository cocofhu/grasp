package models

import "github.com/cocofhu/grasp/internal/modelrouting"

// ModelConnection stores a non-secret service connection and credential reference.
type ModelConnection modelrouting.Connection

// ModelConfiguration persists a coding tool and concrete model binding.
type ModelConfiguration modelrouting.Configuration
