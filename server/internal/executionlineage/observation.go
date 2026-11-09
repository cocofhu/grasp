package executionlineage

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"gorm.io/gorm"
)

// Observation is a read-only deployment diagnostic, never a dispatch permit.
// Active is true only when both the external marker and existing SQL binding
// agree. No missing table, database, directory or marker is initialized here.
type Observation struct {
	Version           int    `json:"version"`
	Status            string `json:"status"`
	Active            bool   `json:"active"`
	SQLMatches        bool   `json:"sqlMatches"`
	SQLDatasetPresent bool   `json:"sqlDatasetPresent"`
	NamespaceID       string `json:"namespaceId,omitempty"`
	DatasetID         string `json:"datasetId,omitempty"`
	Lineage           string `json:"lineage,omitempty"`
	Revision          uint64 `json:"revision,omitempty"`
}

// ObserveConfigured performs GET/SELECT only. Nil db means the caller has
// established that the SQL file does not exist, not that SQL is unreachable.
func ObserveConfigured(ctx context.Context, control config.StorageConfig, payloads []config.StorageConfig, protected []string, db *gorm.DB) (Observation, error) {
	view := Observation{Version: 1, Status: "unavailable"}
	_, present, err := observeDataset(ctx, db)
	view.SQLDatasetPresent = present
	if err != nil {
		return view, ErrUnavailable
	}
	reader, err := observationReader(control, payloads, protected)
	if err != nil {
		return view, ErrUnavailable
	}
	return observe(ctx, reader, db, view)
}

func observe(ctx context.Context, reader objectstore.ControlReader, db *gorm.DB, view Observation) (Observation, error) {
	marker, _, err := readMarker(ctx, reader)
	if err != nil {
		if errors.Is(err, ErrUninitialized) {
			view.Status = "uninitialized"
		}
		return view, err
	}
	view.NamespaceID, view.DatasetID, view.Lineage, view.Revision = marker.NamespaceID, marker.DatasetID, marker.Lineage, marker.Revision
	if marker.State != Active {
		view.Status = "inactive"
		return view, ErrInactive
	}
	binding, present, err := observeDataset(ctx, db)
	view.SQLDatasetPresent = present
	if err != nil {
		return view, ErrUnavailable
	}
	if !present || !bindingMatches(binding, marker, marker.Revision) {
		view.Status = "unbound"
		return view, ErrUnbound
	}
	// A concurrent offline rotation cannot turn the earlier read into success.
	after, _, err := readMarker(ctx, reader)
	if err != nil || after != marker {
		return view, ErrUnavailable
	}
	view.Status, view.Active, view.SQLMatches = "active", true, true
	return view, nil
}

func observeDataset(ctx context.Context, db *gorm.DB) (models.ExecutionDataset, bool, error) {
	var binding models.ExecutionDataset
	if db == nil {
		return binding, false, nil
	}
	if err := outsideSQLTransaction(db); err != nil {
		return binding, false, err
	}
	// HasTable discards metadata-query failures; GetTables preserves them.
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

func observationReader(control config.StorageConfig, payloads []config.StorageConfig, protected []string) (objectstore.ControlReader, error) {
	driver := strings.ToLower(strings.TrimSpace(control.Driver))
	if driver == "local" {
		if strings.TrimSpace(control.BlobsRoot) == "" {
			return nil, ErrUnavailable
		}
		root, err := physicalPath(control.BlobsRoot)
		if err != nil {
			return nil, ErrUnavailable
		}
		paths := append([]string(nil), protected...)
		for _, payload := range payloads {
			path := payload.BlobsRoot
			if path == "" {
				path = filepath.Join("data", "blobs")
			}
			paths = append(paths, path)
		}
		for _, path := range paths {
			if path == "" {
				continue
			}
			other, err := physicalPath(path)
			if err != nil || pathOverlap(root, other) {
				return nil, ErrUnavailable
			}
		}
		return objectstore.NewLocalControlReader(root)
	}
	if driver != "s3" && driver != "cos" {
		return nil, ErrUnavailable
	}
	for _, payload := range payloads {
		payloadDriver := strings.ToLower(strings.TrimSpace(payload.Driver))
		// A read-only check cannot run the alias-separation write probe. Require
		// disjoint logical prefixes even if endpoints or buckets differ.
		if (payloadDriver == "s3" || payloadDriver == "cos") && prefixOverlap(control.S3.Prefix, payload.S3.Prefix) {
			return nil, ErrUnavailable
		}
	}
	opts := control.S3
	if driver == "cos" && opts.PathStyle == nil {
		pathStyle := false
		opts.PathStyle = &pathStyle
	}
	return objectstore.NewS3(opts)
}
