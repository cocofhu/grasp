package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

func soloCreationKey(user, requestID string) string {
	hash := sha256.Sum256([]byte(user + "\x00" + requestID))
	return hex.EncodeToString(hash[:])
}

func validateSoloCreationID(id string) error {
	if id == "" || len(id) > 128 || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		return soloErr(400, "invalid_creation_identity", "创建请求标识无效")
	}
	return nil
}

func soloCreationFingerprint(in SoloCreateInput, continuationRun string) (string, error) {
	in.ClientRequestID = ""
	body, err := json.Marshal(struct {
		Input        SoloCreateInput
		Continuation string
	}{in, continuationRun})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:]), nil
}

func (s *SoloService) findSoloCreation(ctx context.Context, user, requestID, fingerprint string) (*SoloSessionView, bool, error) {
	var receipt models.SoloCreation
	err := s.db.WithContext(ctx).First(&receipt, "id = ?", soloCreationKey(user, requestID)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if receipt.UserID != user {
		return nil, true, soloErr(404, "session_not_found", "Solo 会话不存在")
	}
	if fingerprint != "" && receipt.Fingerprint != fingerprint {
		return nil, true, soloErr(409, "creation_identity_conflict", "同一创建标识不能用于不同内容")
	}
	var parent models.SoloSession
	if err := s.db.WithContext(ctx).Select("id", "user_id").First(&parent, "id = ?", receipt.SessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, true, soloErr(410, "created_session_deleted", "此次创建的会话已删除，请发起新的会话")
		}
		return nil, true, err
	}
	if parent.UserID != user {
		return nil, true, soloErr(404, "session_not_found", "Solo 会话不存在")
	}
	view, err := s.Get(ctx, parent.ID, user)
	if view != nil {
		view.ClientRequestID, view.InitialTurnID = requestID, receipt.InitialTurnID
	}
	return view, true, err
}

// ResolveCreation is observational: it never provisions or resumes compute.
func (s *SoloService) ResolveCreation(ctx context.Context, user, requestID string) (*SoloSessionView, error) {
	if err := validateSoloCreationID(requestID); err != nil {
		return nil, err
	}
	view, found, err := s.findSoloCreation(ctx, user, requestID, "")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, soloErr(404, "creation_not_found", "未找到此次创建请求")
	}
	return view, nil
}

func (s *SoloService) recordExistingSoloCreation(ctx context.Context, user string, in SoloCreateInput, runID, sessionID string) (*SoloSessionView, error) {
	fingerprint, err := soloCreationFingerprint(in, runID)
	if err != nil {
		return nil, err
	}
	err = s.executionTransaction(ctx, sessionID, func(tx *gorm.DB) error {
		if err := lockSoloCompute(tx); err != nil {
			return err
		}
		var previous models.SoloCreation
		if err := tx.First(&previous, "id = ?", soloCreationKey(user, in.ClientRequestID)).Error; err == nil {
			if previous.UserID != user || previous.Fingerprint != fingerprint {
				return soloErr(409, "creation_identity_conflict", "同一创建标识不能用于不同内容")
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := LockWorkflowAdmission(tx); err != nil {
			return err
		}
		actual, err := workflowChat(tx, runID, user)
		if err != nil {
			return err
		}
		if actual != sessionID {
			return soloErr(409, "session_changed", "续聊会话已变化，请重新加载")
		}
		var parent models.SoloSession
		if err := tx.Select("user_id").First(&parent, "id = ?", sessionID).Error; err != nil {
			return err
		}
		if parent.UserID != user {
			return soloErr(404, "session_not_found", "Solo 会话不存在")
		}
		return tx.Create(&models.SoloCreation{ID: soloCreationKey(user, in.ClientRequestID), UserID: user, Fingerprint: fingerprint, SessionID: sessionID}).Error
	})
	if err != nil {
		return nil, err
	}
	view, _, err := s.findSoloCreation(ctx, user, in.ClientRequestID, fingerprint)
	return view, err
}

func (s *SoloService) initialSoloTurn(ctx context.Context, row *models.SoloSession, in SoloCreateInput, references []models.MessageReference) (*soloTurn, error) {
	if strings.TrimSpace(in.Text) == "" && len(in.Images) == 0 && len(references) == 0 {
		return nil, nil
	}
	turn := &soloTurn{ID: uuid.NewString(), ActorID: row.UserID, ClientMessageID: "create:" + soloCreationKey(row.UserID, in.ClientRequestID), Text: strings.TrimSpace(in.Text), Images: in.Images, References: references}
	if err := s.queueFingerprint(turn, nil); err != nil {
		return nil, err
	}
	images, err := blob.IngestPromptImages(ctx, s.blobs, turn.Images)
	if err != nil {
		return nil, err
	}
	turn.Images = images
	return turn, nil
}

func (s *SoloService) prepareInitialSoloTurn(ctx context.Context, row *models.SoloSession, turn *soloTurn) (*chatsession.PreparedIntent, error) {
	if turn == nil {
		return nil, nil
	}
	intent, err := s.durableSoloIntent(turn)
	if err != nil {
		return nil, err
	}
	prepared, err := s.durable.PrepareIntent(ctx, soloQueueScope(row.ID), intent)
	return &prepared, err
}

func (s *SoloService) admitInitialSoloTurn(_ context.Context, tx *gorm.DB, row *models.SoloSession, prepared *chatsession.PreparedIntent) error {
	if prepared == nil {
		return nil
	}
	// The most outer creation transaction commits the session and this receipt
	// together. Publication already completed before that transaction began.
	_, err := s.durable.AdmitPreparedTx(tx, soloQueueScope(row.ID), *prepared, soloQueueCapacity, func(tx *gorm.DB) error { return s.soloQueueAdmission(tx, row.ID) })
	return err
}

// A crash between acceptance and sandbox reservation leaves a known, durable
// creation intent. Only these explicitly requested, still-starting sessions
// are provisioned; history reads never invoke this recovery path.
func (s *SoloService) restoreUnboundSoloCreations() {
	if s.sbx == nil || s.agents == nil {
		return
	}
	s.restoreSoloRestartIntents()
	var rows []models.SoloSession
	if err := s.db.Where("status = ? AND lifecycle_action = '' AND sandbox_id = 0 AND EXISTS (SELECT 1 FROM solo_creations c WHERE c.session_id = solo_sessions.id)", models.SoloStatusStarting).Order("created_at,id").Limit(16).Find(&rows).Error; err != nil {
		log.Warn().Err(err).Msg("Solo creation recovery deferred")
		return
	}
	for i := range rows {
		if err := s.openSandbox(context.Background(), &rows[i]); err != nil {
			soloLifecycleQuery(s.db, &rows[i]).Where("status = ? AND lifecycle_action = ''", models.SoloStatusStarting).Updates(map[string]any{"status": models.SoloStatusError, "error": truncErr(err)})
		}
	}
}
