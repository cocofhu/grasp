package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ArtifactTurn is frozen before starting an import and revalidated before
// publishing. A late upload must never attach itself to the next human turn.
type ArtifactTurn struct {
	SessionID  string
	TurnID     string
	Lane       string
	SandboxID  uint
	StateRunID uint
	Iteration  int
}

type ArtifactDeliveryService struct {
	db          *gorm.DB
	Arts        *ArtifactService
	OpenSource  func(context.Context, string, string, string) (io.ReadCloser, string, error)
	CurrentTurn func(string, string) (ArtifactTurn, error)
	Published   func(string, string, ArtifactTurn, models.ArtifactAttachment)
}

func NewArtifactDeliveryService(db *gorm.DB, arts *ArtifactService, preview *PreviewService) *ArtifactDeliveryService {
	s := &ArtifactDeliveryService{db: db, Arts: arts}
	s.OpenSource = func(ctx context.Context, runID, nodeID, path string) (io.ReadCloser, string, error) {
		if preview == nil || preview.mgr == nil {
			return nil, "", errors.New("sandbox file export is unavailable")
		}
		row, ok := preview.PreviewSandbox(runID, nodeID)
		if !ok || row.Status != "running" {
			return nil, "", errors.New("authorized sandbox is not active")
		}
		sb, err := preview.mgr.AttachNoEnsure(ctx, row.Name)
		if err != nil {
			return nil, "", err
		}
		r, err := sb.OpenArtifactFile(ctx, path)
		session := ""
		if row.Purpose == SandboxPurposeSolo {
			session = row.ThreadID
		}
		return r, session, err
	}
	return s
}

func attachmentOf(a models.Artifact) models.ArtifactAttachment {
	rev := a.Revision
	if rev < 1 {
		rev = 1
	}
	return models.ArtifactAttachment{ArtifactID: a.ID, Revision: rev, Name: a.Name, Kind: a.Kind, MimeType: a.MimeType, SizeBytes: a.SizeBytes}
}

func (s *ArtifactDeliveryService) Import(ctx context.Context, runID, nodeID, path, name string) (models.ArtifactAttachment, error) {
	if strings.TrimSpace(path) == "" {
		return models.ArtifactAttachment{}, errors.New("path is required")
	}
	if name == "" {
		name = filepath.Base(path)
	}
	r, session, err := s.OpenSource(ctx, runID, nodeID, path)
	if err != nil {
		return models.ArtifactAttachment{}, err
	}
	defer func() { _ = r.Close() }()
	a, err := s.Arts.SaveFile(ctx, runID, nodeID, name, session, r)
	return attachmentOf(a), err
}

func (s *ArtifactDeliveryService) current(runID, nodeID string) (ArtifactTurn, error) {
	if s.CurrentTurn == nil {
		return ArtifactTurn{}, errors.New("file delivery has no active conversation")
	}
	turn, err := s.CurrentTurn(runID, nodeID)
	if err != nil {
		return turn, err
	}
	if turn.TurnID == "" {
		return turn, errors.New("file delivery requires an active turn")
	}
	return turn, nil
}

func (s *ArtifactDeliveryService) SendFile(ctx context.Context, runID, nodeID, path, name, caption string) (models.ArtifactAttachment, error) {
	if len([]rune(strings.TrimSpace(caption))) > 1000 {
		return models.ArtifactAttachment{}, errors.New("caption exceeds 1000 characters")
	}
	turn, err := s.current(runID, nodeID)
	if err != nil {
		return models.ArtifactAttachment{}, err
	}
	a, err := s.Import(ctx, runID, nodeID, path, name)
	if err != nil {
		return a, err
	}
	return s.deliver(ctx, runID, nodeID, turn, a, caption)
}

func (s *ArtifactDeliveryService) SendExisting(ctx context.Context, runID, nodeID, id string, revision int, caption string) (models.ArtifactAttachment, error) {
	turn, err := s.current(runID, nodeID)
	if err != nil {
		return models.ArtifactAttachment{}, err
	}
	file, err := s.Arts.OpenFile(ctx, id, revision)
	if err != nil {
		return models.ArtifactAttachment{}, err
	}
	defer func() { _ = file.Reader.Close() }()
	if file.Artifact.RunID != runID {
		return models.ArtifactAttachment{}, errors.New("artifact not found in current scope")
	}
	a := attachmentOf(file.Artifact)
	a.MimeType = file.MimeType
	return s.deliver(ctx, runID, nodeID, turn, a, caption)
}

func (s *ArtifactDeliveryService) deliver(ctx context.Context, runID, nodeID string, turn ArtifactTurn, a models.ArtifactAttachment, caption string) (models.ArtifactAttachment, error) {
	current, err := s.current(runID, nodeID)
	if err != nil || current != turn {
		return a, errors.New("the originating turn ended; file was saved but was not sent to another turn")
	}
	caption = strings.TrimSpace(caption)
	if len([]rune(caption)) > 1000 {
		return a, errors.New("caption exceeds 1000 characters")
	}
	a.Caption = caption
	key := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", runID, nodeID, turn.TurnID, a.ArtifactID, a.Revision)))
	row := models.ArtifactDelivery{ID: "delivery-" + hex.EncodeToString(key[:]), RunID: runID, NodeID: nodeID, TurnID: turn.TurnID, SessionID: turn.SessionID, StateRunID: turn.StateRunID, Iteration: turn.Iteration, ArtifactAttachment: a, CreatedAt: time.Now()}
	inserted := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if result.Error != nil {
			return result.Error
		}
		inserted = result.RowsAffected > 0
		if !inserted {
			return nil
		}
		if turn.SessionID != "" {
			id := "reply:" + turn.TurnID
			placeholder := soloMessageSQL{ID: id, SessionID: turn.SessionID, Role: "assistant", TurnID: turn.TurnID, CreatedAt: time.Now()}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&placeholder).Error; err != nil {
				return err
			}
			// Serialize file edits on the same reply row as final publication.
			// Never load or Save its content/structured bodies under this lock.
			if err := tx.Model(&soloMessageSQL{}).Where("id = ? AND session_id = ?", id, turn.SessionID).UpdateColumn("turn_id", turn.TurnID).Error; err != nil {
				return err
			}
			var reply soloMessageSQL
			if err := tx.Select("id", "files").First(&reply, "id = ? AND session_id = ?", id, turn.SessionID).Error; err != nil {
				return err
			}
			var files []models.ArtifactAttachment
			if reply.Files != nil && *reply.Files != "" {
				if err := json.Unmarshal([]byte(*reply.Files), &files); err != nil {
					return err
				}
			}
			encoded, err := messageJSON(append(files, a))
			if err != nil {
				return err
			}
			return tx.Model(&soloMessageSQL{}).Where("id = ? AND session_id = ?", id, turn.SessionID).Update("files", encoded).Error
		}
		if turn.StateRunID != 0 {
			if err := tx.Table("state_runs").Where("id = ?", turn.StateRunID).UpdateColumn("has_file_deliveries", true).Error; err != nil {
				return err
			}
		}
		return tx.Table("react_conversations").Where("run_id = ? AND node_id = ? AND iteration = ?", runID, nodeID, turn.Iteration).UpdateColumn("has_file_deliveries", true).Error
	})
	if err != nil {
		return a, err
	}
	if !inserted {
		if err := s.db.WithContext(ctx).First(&row, "id = ?", row.ID).Error; err != nil {
			return a, err
		}
		a = row.ArtifactAttachment
	} else if s.Published != nil {
		s.Published(runID, nodeID, turn, a)
	}
	return a, nil
}

func ArtifactTurnFiles(db *gorm.DB, runID, nodeID, turnID string) []models.ArtifactAttachment {
	var deliveries []models.ArtifactDelivery
	if db.Where("run_id = ? AND node_id = ? AND turn_id = ?", runID, nodeID, turnID).Order("created_at,id").Find(&deliveries).Error != nil {
		return nil
	}
	files := make([]models.ArtifactAttachment, 0, len(deliveries))
	for _, d := range deliveries {
		files = append(files, d.ArtifactAttachment)
	}
	return files
}
