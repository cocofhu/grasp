// Package payloadstore wires payloads, attachments and snapshots to their
// verified physical primary. Storage changes require an explicit offline copy.
//
// It is the composition layer: it selects the objectstore backend, attaches
// filestore to it, and proves the SQL references and the primary agree.
package payloadstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/objectstore"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Location is a logical storage location, not a credential. The primary logical
// identity survives verified physical migrations. Endpoint/bucket/prefix changes
// must go through migration instead of making existing references unreadable.
type Location struct {
	Name        string `gorm:"primaryKey"`
	Driver      string
	Endpoint    string
	Region      string
	Bucket      string
	Prefix      string
	LocalRoot   string
	Fingerprint string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (Location) TableName() string { return "payload_storage_locations" }

type Storage struct {
	Blobs    blob.Store
	Files    *filestore.Store
	Objects  objectstore.Store
	Location Location
}

func proposedLocation(cfg config.StorageConfig) (Location, error) {
	root := cfg.BlobsRoot
	if root == "" {
		root = "data/blobs"
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Location{}, err
	}
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver == "" {
		driver = "local"
	}
	location := Location{Name: "primary", Driver: driver, LocalRoot: abs}
	identity := "local\x00" + abs
	if driver == "s3" || driver == "cos" {
		location.Driver = "s3"
		location.Endpoint = strings.TrimSuffix(strings.TrimSpace(cfg.S3.Endpoint), "/")
		location.Bucket, location.Prefix, location.Region = cfg.S3.Bucket, strings.Trim(cfg.S3.Prefix, "/"), cfg.S3.Region
		identity = location.Endpoint + "\x00" + location.Bucket + "\x00" + location.Prefix
	}
	hash := sha256.Sum256([]byte(identity))
	location.Fingerprint = hex.EncodeToString(hash[:])
	return location, nil
}

func bindLocation(db *gorm.DB, target Location) error {
	if err := db.AutoMigrate(&Location{}, &PrimaryClosure{}); err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var current Location
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "name = ?", "primary").Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if target.Driver == "s3" {
				return BindEmptyPrimaryClosureTx(tx.Statement.Context, tx, target)
			}
			return tx.Create(&target).Error
		}
		if err != nil {
			return err
		}
		if current.Fingerprint != target.Fingerprint || current.Driver != target.Driver {
			return errors.New("payload storage location changed: migrate and verify existing objects before switching storage")
		}
		return nil
	})
}

func Open(ctx context.Context, db *gorm.DB, cfg *config.Config) (*Storage, error) {
	if cfg == nil {
		return nil, errors.New("storage config required")
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		return nil, err
	}
	location, err := proposedLocation(cfg.Storage)
	if err != nil {
		return nil, err
	}
	result := &Storage{Files: files, Location: location}
	var remote *objectstore.S3
	switch location.Driver {
	case "local":
	case "s3":
		remote, err = newObjects(cfg.Storage)
		if err != nil {
			return nil, err
		}
		result.Objects = remote
	default:
		return nil, errors.New("unsupported payload storage driver")
	}
	if err = bindLocation(db.WithContext(ctx), location); err != nil {
		return nil, err
	}
	if err = CheckPrimaryClosure(ctx, db, cfg.Storage); err != nil {
		return nil, err
	}
	if result.Objects != nil {
		probe, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = remote.Probe(probe)
		cancel()
		if err != nil {
			return nil, err
		}
		result.Objects = objectstore.FencePublications(result.Objects)
	}
	if err = files.SetObjectStore(result.Objects); err != nil {
		return nil, err
	}
	result.Blobs, err = blob.NewWithObjectStore(cfg, result.Objects)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Close stops all file/attachment publications before runtime unregistration.
// Read-only handles remain valid; no source objects are removed.
func (s *Storage) Close() {
	s.Files.Close()
	if local, ok := s.Blobs.(*blob.LocalFS); ok {
		local.Close()
	}
	if publications, ok := s.Objects.(*objectstore.Publications); ok {
		publications.Close()
	}
}

// OpenObjects validates a destination without changing its SQL location.
func OpenObjects(ctx context.Context, cfg config.StorageConfig) (objectstore.Store, error) {
	objects, err := newObjects(cfg)
	if err != nil {
		return nil, err
	}
	probe, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err = objects.Probe(probe); err != nil {
		return nil, err
	}
	return objects, nil
}

func newObjects(cfg config.StorageConfig) (*objectstore.S3, error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.Driver))
	if driver != "s3" && driver != "cos" {
		return nil, errors.New("replica destination must use s3 or cos")
	}
	options := cfg.S3
	if options.PathStyle == nil && driver == "cos" {
		pathStyle := false
		options.PathStyle = &pathStyle
	}
	return objectstore.NewS3(options)
}
