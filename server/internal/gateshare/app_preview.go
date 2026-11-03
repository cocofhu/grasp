package gateshare

import (
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// CreateAppPreview shares one app independently of any approval link.
func (s *Service) CreateAppPreview(runID, nodeID string, sandboxID uint, port, iteration int, mode, owner, origin string) (*CreateResult, error) {
	token, err := GenerateToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tier, ttl, _ := ParseTTLTier("")
	link := models.GateShareLink{
		ID: newShareID(), TokenHash: HashToken(token), Kind: models.ShareLinkKindAppPreview,
		RunID: runID, NodeID: nodeID, PreviewSandboxID: sandboxID, PreviewPort: port,
		PreviewChatMode: mode, Iteration: iteration,
		CreatedBy: owner, TTLTier: tier, PermissionPreset: models.SharePermissionReactOnly,
		ExpiresAt: now.Add(ttl), CreatedAt: now,
	}
	var revoked []string
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		revoked, err = revokeAppPreview(tx, runID, nodeID, sandboxID, port, mode, now)
		if err != nil {
			return err
		}
		return tx.Create(&link).Error
	})
	if err != nil {
		return nil, err
	}
	s.notifyInvalidated(revoked)
	return &CreateResult{
		ID: link.ID, URL: strings.TrimRight(origin, "/") + "/public/approvals/app#t=" + token,
		TTLTier: tier, PermissionPreset: link.PermissionPreset, ExpiresAt: link.ExpiresAt, State: models.ShareLinkStateActive,
	}, nil
}

func revokeAppPreview(db *gorm.DB, runID, nodeID string, sandboxID uint, port int, mode string, now time.Time) ([]string, error) {
	scope := db.Model(&models.GateShareLink{}).
		Where("kind = ? AND run_id = ? AND node_id = ? AND preview_sandbox_id = ? AND preview_port = ? AND preview_chat_mode = ? AND revoked_at IS NULL AND expires_at > ?",
			models.ShareLinkKindAppPreview, runID, nodeID, sandboxID, port, mode, now)
	var hashes []string
	if err := scope.Session(&gorm.Session{}).Pluck("token_hash", &hashes).Error; err != nil {
		return nil, err
	}
	return hashes, scope.Update("revoked_at", now).Error
}

func (s *Service) RevokeAppPreview(runID, nodeID string, sandboxID uint, port int, mode string) error {
	hashes, err := revokeAppPreview(s.db, runID, nodeID, sandboxID, port, mode, time.Now())
	if err == nil {
		s.notifyInvalidated(hashes)
	}
	return err
}

func (s *Service) LookupAppPreview(token string) (*models.GateShareLink, bool) {
	if !ValidTokenShape(token) {
		return nil, false
	}
	hash := HashToken(token)
	link, err := s.LoadLinkByTokenHash(hash)
	if err != nil || link == nil || link.Kind != models.ShareLinkKindAppPreview ||
		!EqualHash(link.TokenHash, hash) || linkState(link, time.Now()) != models.ShareLinkStateActive {
		return nil, false
	}
	return link, true
}
