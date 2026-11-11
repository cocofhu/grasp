package executionlineage

import (
	"context"
	"errors"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"gorm.io/gorm"
)

// Observation is a SELECT-only diagnostic, never an execution permit. It
// neither initializes missing SQL metadata nor contacts any object store.
type Observation struct {
	Version           int    `json:"version"`
	Status            string `json:"status"`
	Active            bool   `json:"active"`
	SQLMatches        bool   `json:"sqlMatches"`
	SQLDatasetPresent bool   `json:"sqlDatasetPresent"`
	MaintenanceActive bool   `json:"maintenanceActive"`
	NamespaceID       string `json:"namespaceId,omitempty"`
	DatasetID         string `json:"datasetId,omitempty"`
	Lineage           string `json:"lineage,omitempty"`
	Revision          uint64 `json:"revision,omitempty"`
}

func ObserveSQL(ctx context.Context, db *gorm.DB) (Observation, error) {
	view := Observation{Version: 1, Status: "unavailable"}
	if db == nil {
		view.Status = "uninitialized"
		return view, ErrUninitialized
	}
	if err := outsideSQLTransaction(db); err != nil {
		return view, err
	}
	tables, err := db.WithContext(ctx).Migrator().GetTables()
	if err != nil {
		return view, ErrUnavailable
	}
	if !containsTable(tables, "execution_datasets") {
		view.Status = "uninitialized"
		return view, ErrUninitialized
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// One snapshot without row locks: diagnostics must never queue behind
		// or block runtime admission and offline administration.
		var control storageguard.Control
		if err := tx.Take(&control, "name = ?", "primary").Error; err != nil {
			return ErrUnavailable
		}
		view.MaintenanceActive = control.Owner != ""
		var binding models.ExecutionDataset
		err := tx.Take(&binding, "id = ?", "primary").Error
		if err == nil {
			view.SQLDatasetPresent = true
			marker, valid := markerFromBinding(binding)
			if valid != nil {
				view.Status = "unbound"
				return ErrUnbound
			}
			view.NamespaceID, view.DatasetID, view.Lineage, view.Revision = marker.NamespaceID, marker.DatasetID, marker.Lineage, marker.Revision
			view.SQLMatches = true
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUnavailable
		}
		if err := pendingRecoveryCheck(tx, false); err != nil {
			if errors.Is(err, ErrInactive) {
				view.Status = "inactive"
			}
			return err
		}
		if view.MaintenanceActive {
			view.Status = "inactive"
			return ErrInactive
		}
		if !view.SQLDatasetPresent {
			view.Status = "uninitialized"
			return ErrUninitialized
		}
		view.Status, view.Active = "active", true
		return nil
	})
	return view, err
}

func observeDataset(ctx context.Context, db *gorm.DB) (models.ExecutionDataset, bool, error) {
	var binding models.ExecutionDataset
	if db == nil {
		return binding, false, nil
	}
	if err := outsideSQLTransaction(db); err != nil {
		return binding, false, err
	}
	tables, err := db.WithContext(ctx).Migrator().GetTables()
	if err != nil {
		return binding, false, err
	}
	if !containsTable(tables, "execution_datasets") {
		return binding, false, nil
	}
	err = db.WithContext(ctx).Take(&binding, "id = ?", "primary").Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return binding, false, nil
	}
	return binding, err == nil, err
}

func containsTable(tables []string, name string) bool {
	for _, table := range tables {
		if table == name {
			return true
		}
	}
	return false
}
