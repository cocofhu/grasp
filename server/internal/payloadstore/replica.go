package payloadstore

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/storageguard"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ReplicaHeader struct {
	Version        int       `json:"version"`
	ID             string    `json:"id"`
	Source         Location  `json:"source"`
	Destination    Location  `json:"destination"`
	DatabaseSHA256 string    `json:"databaseSHA256"`
	CreatedAt      time.Time `json:"createdAt"`
}

type ReplicaSummary struct {
	Objects    objectstore.ArchiveReport `json:"objects"`
	References ReferenceReport           `json:"references"`
	SHA256     string                    `json:"sha256"`
}

type inventoryLine struct {
	Type     string                     `json:"type"`
	Header   *ReplicaHeader             `json:"header,omitempty"`
	Object   *objectstore.ReplicaObject `json:"object,omitempty"`
	Complete *ReplicaSummary            `json:"complete,omitempty"`
}

func validateReplicaHeader(header ReplicaHeader) error {
	if header.Version != 1 || len(header.DatabaseSHA256) != 64 || header.Destination.Driver != "s3" {
		return errors.New("invalid replica header")
	}
	for _, location := range []Location{header.Source, header.Destination} {
		if location.Driver != "local" && location.Driver != "s3" {
			return errors.New("invalid replica location driver")
		}
		normalized, err := proposedLocation(config.StorageConfig{Driver: location.Driver, BlobsRoot: location.LocalRoot,
			S3: config.S3StorageConfig{Endpoint: location.Endpoint, Bucket: location.Bucket, Prefix: location.Prefix, Region: location.Region}})
		if err != nil || normalized.Fingerprint != location.Fingerprint || normalized.Endpoint != location.Endpoint || normalized.Prefix != location.Prefix {
			return errors.New("replica location identity is inconsistent")
		}
	}
	return nil
}

// Replica records a verified copy; location changes retain the previous
// identity and proof. This catalog never authorizes deleting the source.
type Replica struct {
	ID                     string `gorm:"primaryKey"`
	SourceFingerprint      string
	DestinationFingerprint string
	DatabaseSHA256         string
	ManifestSHA256         string
	Objects                int64
	Bytes                  int64
	CreatedAt              time.Time
}

func (Replica) TableName() string { return "payload_storage_replicas" }

func Locations(cfg config.StorageConfig, target config.StorageConfig) (Location, Location, error) {
	source, err := proposedLocation(cfg)
	if err != nil {
		return source, Location{}, err
	}
	destination, err := proposedLocation(target)
	if err != nil {
		return source, destination, err
	}
	if destination.Driver != "s3" {
		return source, destination, errors.New("replica destination must use s3 or cos")
	}
	if source.Driver == "s3" && source.Endpoint == destination.Endpoint && source.Bucket == destination.Bucket {
		a, b := source.Prefix, destination.Prefix
		if a == "" || b == "" || a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/") {
			return source, destination, errors.New("source and destination object prefixes overlap")
		}
	}
	return source, destination, nil
}

func CheckSource(ctx context.Context, db *gorm.DB, owner string, source Location) error {
	if err := storageguard.Check(ctx, db, owner); err != nil {
		return err
	}
	var current Location
	if err := db.WithContext(ctx).First(&current, "name = ?", "primary").Error; err != nil {
		return err
	}
	if current.Fingerprint != source.Fingerprint {
		return errors.New("source configuration does not match the registered storage location")
	}
	return nil
}

// WriteReplica creates a bounded-memory JSONL inventory only after every source
// object and destination GET have passed SHA-256 verification. SQL references
// are checked against the destination with no legacy-local fallback.
func WriteReplica(ctx context.Context, db *gorm.DB, source, destination objectstore.Store, header ReplicaHeader, output io.Writer) (ReplicaSummary, error) {
	var summary ReplicaSummary
	if err := CheckSource(ctx, db, header.ID, header.Source); err != nil {
		return summary, err
	}
	if err := validateReplicaHeader(header); err != nil {
		return summary, err
	}
	h := sha256.New()
	encoder := json.NewEncoder(io.MultiWriter(output, h))
	if err := encoder.Encode(inventoryLine{Type: "header", Header: &header}); err != nil {
		return summary, err
	}
	objects, err := objectstore.Replicate(ctx, source, destination, func(entry objectstore.ReplicaObject) error {
		return encoder.Encode(inventoryLine{Type: "object", Object: &entry})
	})
	if err != nil {
		return summary, err
	}
	refs, err := VerifyReferences(ctx, db, destination)
	if err != nil {
		return summary, err
	}
	if err = CheckSource(ctx, db, header.ID, header.Source); err != nil {
		return summary, err
	}
	summary = ReplicaSummary{Objects: objects, References: refs, SHA256: hex.EncodeToString(h.Sum(nil))}
	err = json.NewEncoder(output).Encode(inventoryLine{Type: "complete", Complete: &summary})
	return summary, err
}

// VerifyReplica refuses incomplete/truncated inventories, duplicate/reordered
// keys, changed bytes/metadata, and dangling SQL references. Unlisted extras are
// retained; this manifest is deliberately not a garbage-collection allowlist.
func VerifyReplica(ctx context.Context, db *gorm.DB, destination objectstore.Store, input io.Reader) (ReplicaHeader, ReplicaSummary, error) {
	var header ReplicaHeader
	var summary ReplicaSummary
	reader := bufio.NewScanner(input)
	reader.Buffer(make([]byte, 32<<10), 1<<20)
	h, previous, complete := sha256.New(), "", false
	lineNumber := 0
	var objects objectstore.ArchiveReport
	for reader.Scan() {
		if complete {
			return header, summary, errors.New("unexpected bytes after replica completion")
		}
		lineNumber++
		line := inventoryLine{}
		if err := json.Unmarshal(reader.Bytes(), &line); err != nil {
			return header, summary, errors.New("invalid replica inventory JSON")
		}
		switch line.Type {
		case "header":
			if lineNumber != 1 || line.Header == nil || line.Header.Version != 1 {
				return header, summary, errors.New("invalid replica inventory header")
			}
			header = *line.Header
			if err := validateReplicaHeader(header); err != nil {
				return header, summary, err
			}
			if err := storageguard.Check(ctx, db, header.ID); err != nil {
				return header, summary, err
			}
		case "object":
			if lineNumber <= 1 || line.Object == nil || line.Object.Key <= previous {
				return header, summary, errors.New("invalid replica inventory ordering")
			}
			if err := objectstore.VerifyReplicaObject(ctx, destination, *line.Object); err != nil {
				return header, summary, fmt.Errorf("verify replica %s: %w", line.Object.Key, err)
			}
			previous = line.Object.Key
			objects.Files++
			objects.Bytes += line.Object.Size
		case "complete":
			if lineNumber <= 1 || line.Complete == nil {
				return header, summary, errors.New("invalid replica inventory completion")
			}
			summary = *line.Complete
			if summary.Objects != objects || summary.SHA256 != hex.EncodeToString(h.Sum(nil)) {
				return header, summary, errors.New("replica inventory checksum/count mismatch")
			}
			complete = true
			continue
		default:
			return header, summary, errors.New("unknown replica inventory record")
		}
		if err := hashLine(h, reader.Bytes()); err != nil {
			return header, summary, err
		}
	}
	if err := reader.Err(); err != nil {
		return header, summary, err
	}
	if !complete {
		return header, summary, errors.New("replica inventory is incomplete")
	}
	refs, err := VerifyReferences(ctx, db, destination)
	if err != nil {
		return header, summary, err
	}
	if refs != summary.References {
		return header, summary, errors.New("SQL reference inventory differs from backup")
	}
	return header, summary, storageguard.Check(ctx, db, header.ID)
}

func hashLine(h hash.Hash, line []byte) error {
	_, err := h.Write(line)
	if err == nil {
		_, err = h.Write([]byte{'\n'})
	}
	return err
}

// ActivateReplica is called only after a complete inventory and SQL reference
// verification. The previous physical location remains cataloged and untouched.
func ActivateReplica(ctx context.Context, db *gorm.DB, header ReplicaHeader, summary ReplicaSummary) error {
	if err := validateReplicaHeader(header); err != nil {
		return err
	}
	if len(summary.SHA256) != 64 {
		return errors.New("verified replica proof required")
	}
	if err := db.WithContext(ctx).AutoMigrate(&Replica{}); err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Take the same writer lock as registration, release and Enter.
		locked := tx.Model(&storageguard.Control{}).Where("name = ? AND owner = ?", "primary", header.ID).
			UpdateColumn("revision", gorm.Expr("revision + 1"))
		if locked.Error != nil {
			return locked.Error
		}
		if locked.RowsAffected != 1 {
			return errors.New("payload storage maintenance ownership lost")
		}
		var current Location
		if err := tx.First(&current, "name = ?", "primary").Error; err != nil {
			return err
		}
		if current.Fingerprint != header.Source.Fingerprint && current.Fingerprint != header.Destination.Fingerprint {
			return errors.New("storage location changed since the replica was verified")
		}
		previous := header.Source
		previous.Name = "previous-" + previous.Fingerprint
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&previous).Error; err != nil {
			return err
		}
		proof := Replica{ID: header.ID, SourceFingerprint: header.Source.Fingerprint,
			DestinationFingerprint: header.Destination.Fingerprint, DatabaseSHA256: header.DatabaseSHA256,
			ManifestSHA256: summary.SHA256, Objects: summary.Objects.Files, Bytes: summary.Objects.Bytes, CreatedAt: time.Now().UTC()}
		var existing Replica
		err := tx.First(&existing, "id = ?", proof.ID).Error
		if err == nil {
			if existing.SourceFingerprint != proof.SourceFingerprint || existing.DestinationFingerprint != proof.DestinationFingerprint ||
				existing.DatabaseSHA256 != proof.DatabaseSHA256 || existing.ManifestSHA256 != proof.ManifestSHA256 {
				return errors.New("replica proof conflicts with a previous operation")
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Create(&proof).Error; err != nil {
				return err
			}
		} else {
			return err
		}
		destination := header.Destination
		return tx.Model(&Location{}).Where("name = ?", "primary").Updates(map[string]any{
			"driver": destination.Driver, "endpoint": destination.Endpoint, "region": destination.Region,
			"bucket": destination.Bucket, "prefix": destination.Prefix, "local_root": destination.LocalRoot,
			"fingerprint": destination.Fingerprint}).Error
	})
}

func FileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
