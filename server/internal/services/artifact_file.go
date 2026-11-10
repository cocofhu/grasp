package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MaxArtifactFileBytes bounds each sandbox import without buffering the file.
const MaxArtifactFileBytes int64 = 128 << 20

// Lock a persistent namespace owner before looking up a possibly absent name.
// Locking just the artifact row does not serialize two first-time MySQL imports.
func lockArtifactNamespace(tx *gorm.DB, runID string) error {
	var owner struct{ ID string }
	if err := tx.Table("runs").Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", runID).Find(&owner).Error; err != nil {
		return err
	}
	if owner.ID != "" {
		return nil
	}
	var sandbox struct{ ID uint }
	return tx.Table("sandboxes").Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ?", runID).Order("id desc").Limit(1).Find(&sandbox).Error
}

func artifactFileKind(name, media string) string {
	switch {
	case strings.HasPrefix(media, "image/"):
		return "image"
	case strings.HasPrefix(media, "video/"):
		return "video"
	case strings.HasPrefix(media, "audio/"):
		return "audio"
	case media == "application/pdf":
		return "pdf"
	case strings.HasPrefix(media, "text/") || media == "application/json":
		switch strings.ToLower(filepath.Ext(name)) {
		case ".md", ".markdown", ".json", ".yaml", ".yml", ".html", ".htm":
			return mcp.InferWriteArtifactKind(name)
		default:
			return "text"
		}
	default:
		return "file"
	}
}

// SaveFile imports bytes from an already-authorized sandbox. File paths never
// come from the client when serving a download; only the owning record does.
func (s *ArtifactService) SaveFile(ctx context.Context, runID, nodeID, name, sessionID string, reader io.Reader) (models.Artifact, error) {
	if err := messageOutsideTransaction(s.db); err != nil {
		return models.Artifact{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "/\\\x00\r\n") || strings.Contains(name, "..") {
		return models.Artifact{}, errors.New("invalid artifact name")
	}
	if _, reserved := mcp.ExpectedKindForReservedName(name); reserved {
		return models.Artifact{}, errors.New("reserved contract artifacts must use their existing artifact tools")
	}
	store, err := filestore.FromDB(s.db)
	if err != nil {
		return models.Artifact{}, err
	}
	buffer := bufio.NewReader(reader)
	header, _ := buffer.Peek(512)
	media := strings.Split(http.DetectContentType(header), ";")[0]
	if strings.HasPrefix(media, "text/plain") {
		switch strings.ToLower(filepath.Ext(name)) {
		case ".md", ".markdown":
			media = "text/markdown"
		case ".json":
			media = "application/json"
		case ".csv":
			media = "text/csv"
		case ".yaml", ".yml":
			media = "text/yaml"
		}
	}
	kind := artifactFileKind(name, media)
	encoding := filestore.Binary
	if kind == "image" {
		encoding = filestore.Image
	}
	if kind == "markdown" || kind == "text" || kind == "json" || kind == "html" || kind == "yaml" {
		encoding = filestore.Text
	}
	prepared, err := store.PrepareBounded(ctx, buffer, encoding, s.maxFileBytes)
	if err != nil {
		return models.Artifact{}, err
	}
	ref, size := prepared.Ref(), prepared.Size()
	_, digest, _ := filestore.Parse(ref)
	var out models.Artifact
	err = s.executionTransaction(ctx, runID, func(tx *gorm.DB) error {
		if _, err := store.PreparedRef(prepared); err != nil {
			return err
		}
		if err := lockArtifactNamespace(tx, runID); err != nil {
			return err
		}
		var existing artifactSQL
		findErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ? AND name = ?", runID, name).First(&existing).Error
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		now := time.Now()
		if findErr == nil {
			previousDigest := existing.SHA256
			if previousDigest == "" && filestore.IsRef(existing.Content) {
				_, previousDigest, _ = filestore.Parse(existing.Content)
			}
			if previousDigest == digest && existing.Kind == kind {
				if err := tx.Model(&existing).UpdateColumns(map[string]any{"private_file": true, "session_id": sessionID}).Error; err != nil {
					return err
				}
				existing.PrivateFile, existing.SessionID = true, sessionID
				out = existing.logical()
				return nil
			}
			if existing.Revision < 1 {
				existing.Revision = 1
			}
			existing.SHA256 = previousDigest
			if err := archiveArtifactTx(tx, existing); err != nil {
				return err
			}
			existing.Revision++
		} else {
			var run models.Run
			if err := tx.Select("workflow_id", "workflow_name").Where("id = ?", runID).Find(&run).Error; err != nil {
				return err
			}
			existing = artifactSQL{ID: "art-" + uuid.NewString(), RunID: runID, WorkflowID: run.WorkflowID, WorkflowName: run.WorkflowName, Name: name, Revision: 1, CreatedAt: now}
		}
		existing.NodeID, existing.Kind, existing.Content = nodeID, kind, ref
		existing.MimeType, existing.SHA256, existing.SizeBytes, existing.UpdatedAt, existing.SessionID = media, digest, int(size), now, sessionID
		existing.PrivateFile = true
		out = existing.logical()
		return tx.Save(&existing).Error
	})
	out.Content = ""
	return out, err
}

// ArtifactFile includes a seekable stream for Range/HEAD/If-Range serving.
type ArtifactFile struct {
	Artifact models.Artifact
	Reader   io.ReadSeekCloser
	MimeType string
	SHA256   string
}

// OpenFile reads the storage key without invoking the payload serializer, so a
// large media file is never materialized as a string or base64 in memory.
func (s *ArtifactService) OpenFile(ctx context.Context, id string, revision int) (ArtifactFile, error) {
	var row struct {
		models.Artifact
		Stored string `gorm:"column:content"`
	}
	if err := s.db.WithContext(ctx).Table("artifacts").Where("id = ?", id).Take(&row).Error; err != nil {
		return ArtifactFile{}, err
	}
	if row.Revision < 1 {
		row.Revision = 1
	}
	if revision > 0 && revision != row.Revision {
		var version struct {
			models.ArtifactVersion
			Stored string `gorm:"column:content"`
		}
		if err := s.db.WithContext(ctx).Table("artifact_versions").Where("artifact_id = ? AND revision = ?", id, revision).Take(&version).Error; err != nil {
			return ArtifactFile{}, err
		}
		row.Stored = version.Stored
		row.Revision, row.Kind, row.MimeType, row.SHA256, row.SizeBytes, row.NodeID = revision, version.Kind, version.MimeType, version.SHA256, version.SizeBytes, version.NodeID
		row.UpdatedAt = version.CreatedAt
	}
	var reader io.ReadSeekCloser
	digest := row.SHA256
	if filestore.IsRef(row.Stored) {
		store, err := filestore.FromDB(s.db)
		if err != nil {
			return ArtifactFile{}, err
		}
		file, err := store.Open(ctx, row.Stored)
		if err != nil {
			return ArtifactFile{}, err
		}
		reader = file
		_, digest, _ = filestore.Parse(row.Stored)
	} else {
		raw := []byte(row.Stored)
		if row.Kind == "image" {
			decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(row.Stored))
			if err != nil {
				return ArtifactFile{}, err
			}
			raw = decoded
		}
		reader = memoryFile{bytes.NewReader(raw)}
	}
	media := row.MimeType
	if media == "" {
		var header [512]byte
		n, err := reader.Read(header[:])
		if err != nil && err != io.EOF {
			_ = reader.Close()
			return ArtifactFile{}, err
		}
		media = strings.Split(http.DetectContentType(header[:n]), ";")[0]
		if _, err = reader.Seek(0, io.SeekStart); err != nil {
			_ = reader.Close()
			return ArtifactFile{}, err
		}
	}
	if _, _, err := mime.ParseMediaType(media); err != nil {
		media = "application/octet-stream"
	}
	return ArtifactFile{Artifact: row.Artifact, Reader: reader, MimeType: media, SHA256: digest}, nil
}

type memoryFile struct{ *bytes.Reader }

func (memoryFile) Close() error { return nil }
