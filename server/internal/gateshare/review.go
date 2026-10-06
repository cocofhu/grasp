package gateshare

import (
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"gorm.io/gorm"
)

func (s *Service) statusFromReview(link *models.GateShareLink, convDone bool, run models.Run, now time.Time) InboxShareStatus {
	st := InboxShareStatus{
		State:   linkState(link, now),
		HasPass: true,
		HasFail: false,
	}
	if link != nil {
		st.TTLTier = link.TTLTier
		exp := link.ExpiresAt
		st.ExpiresAt = &exp
		st.UsedAt = link.UsedAt
		st.RevokedAt = link.RevokedAt
		if st.State == models.ShareLinkStateActive {
			st.PermissionPreset = link.PermissionPreset
			rem := int64(time.Until(link.ExpiresAt).Seconds())
			if rem < 0 {
				rem = 0
			}
			st.RemainingSec = &rem
		}
	}
	pending := !convDone && !terminalRun(run.Status) && run.Status == "waiting_human"
	switch st.State {
	case models.ShareLinkStateNone, models.ShareLinkStateRevoked, models.ShareLinkStateExpired:
		st.CanCreate = pending
		st.CanManage = false
	case models.ShareLinkStateActive:
		st.CanCreate = false
		st.CanManage = pending
	case models.ShareLinkStateUsed:
		st.CanCreate = false
		st.CanManage = false
	}
	return st
}

func (s *Service) currentPendingReview(runID, nodeID string) (models.ReactConversation, models.Run, error) {
	var run models.Run
	if err := s.db.First(&run, "id = ?", runID).Error; err != nil {
		return models.ReactConversation{}, models.Run{}, ErrReviewNotPending
	}
	if terminalRun(run.Status) {
		return models.ReactConversation{}, run, ErrRunEnded
	}
	node := run.Graph.FindNode(nodeID)
	if !services.IsShareableReviewSession(node) {
		return models.ReactConversation{}, run, ErrNotReviewSession
	}
	var conv models.ReactConversation
	if err := s.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&conv).Error; err != nil {
		return models.ReactConversation{}, run, ErrReviewNotPending
	}
	if conv.Done {
		return conv, run, ErrUsedReadonly
	}
	var sr models.StateRun
	if err := s.db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, nodeID, conv.Iteration).
		Order("id desc").First(&sr).Error; err != nil || sr.Status != "waiting_human" || run.Status != "waiting_human" {
		return conv, run, ErrReviewNotPending
	}
	return conv, run, nil
}

// StatusReview returns leak-free status for an inbox review session.
func (s *Service) StatusReview(runID, nodeID string) (InboxShareStatus, error) {
	st := InboxShareStatus{State: models.ShareLinkStateNone, HasPass: true}
	conv, run, err := s.currentPendingReview(runID, nodeID)
	if err == ErrNotReviewSession {
		return st, err
	}
	if err != nil && conv.ID == 0 {
		var c models.ReactConversation
		if e := s.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
			Order("iteration desc, id desc").First(&c).Error; e != nil {
			if err == ErrRunEnded {
				return st, err
			}
			return st, ErrReviewNotPending
		}
		conv = c
		_ = s.db.First(&run, "id = ?", runID)
	}
	link, lerr := s.latestLink(runID, nodeID, conv.Iteration, models.ShareLinkKindReview)
	if lerr != nil {
		return st, lerr
	}
	st = s.statusFromReview(link, conv.Done, run, time.Now())
	if err == ErrNotReviewSession {
		return st, err
	}
	return st, nil
}

// CreateReview mints a review share link bound to the pending ReactConversation.
func (s *Service) CreateReview(runID, nodeID, ttlTier, permissionPreset, createdBy, publicOrigin string) (*CreateResult, error) {
	conv, run, err := s.currentPendingReview(runID, nodeID)
	if err != nil {
		if conv.ID != 0 && conv.Done {
			return nil, ErrUsedReadonly
		}
		return nil, err
	}
	tier, dur, ok := ParseTTLTier(ttlTier)
	if !ok {
		return nil, ErrInvalidTTL
	}
	preset, ok := ParsePermissionPreset(permissionPreset)
	if !ok {
		return nil, ErrInvalidPermissionPreset
	}
	latest, err := s.latestLink(runID, nodeID, conv.Iteration, models.ShareLinkKindReview)
	if err != nil {
		return nil, err
	}
	if latest != nil && latest.UsedAt != nil {
		return nil, ErrUsedReadonly
	}
	token, err := GenerateToken()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	link := models.GateShareLink{
		ID:               newShareID(),
		TokenHash:        HashToken(token),
		Kind:             models.ShareLinkKindReview,
		RunID:            runID,
		NodeID:           nodeID,
		Iteration:        conv.Iteration,
		CreatedBy:        strings.TrimSpace(createdBy),
		TTLTier:          tier,
		PermissionPreset: preset,
		ExpiresAt:        now.Add(dur),
		CreatedAt:        now,
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := revokeActiveInTx(tx, runID, nodeID, conv.Iteration, now, models.ShareLinkKindReview); err != nil {
			return err
		}
		return tx.Create(&link).Error
	}); err != nil {
		return nil, err
	}
	s.recordShareAudit(run, models.Gate{NodeID: nodeID, Iteration: conv.Iteration}, models.AuditActionGateShareCreate, createdBy, map[string]any{
		"ttlTier":          tier,
		"permissionPreset": preset,
		"expiresAt":        link.ExpiresAt,
		"createdAt":        link.CreatedAt,
		"linkId":           link.ID,
		"kind":             models.ShareLinkKindReview,
	})
	return &CreateResult{
		ID:               link.ID,
		URL:              ShareURL(publicOrigin, token),
		TTLTier:          tier,
		PermissionPreset: preset,
		ExpiresAt:        link.ExpiresAt,
		State:            models.ShareLinkStateActive,
	}, nil
}

// RegenerateReview immediately revokes the active review link and mints a new one.
func (s *Service) RegenerateReview(runID, nodeID, createdBy, publicOrigin string) (*CreateResult, error) {
	conv, run, err := s.currentPendingReview(runID, nodeID)
	if err != nil {
		return nil, err
	}
	latest, err := s.latestLink(runID, nodeID, conv.Iteration, models.ShareLinkKindReview)
	if err != nil {
		return nil, err
	}
	if latest == nil || linkState(latest, time.Now()) != models.ShareLinkStateActive {
		return nil, ErrNotActive
	}
	tier := latest.TTLTier
	preset := latest.PermissionPreset
	token, err := GenerateToken()
	if err != nil {
		return nil, err
	}
	_, dur, ok := ParseTTLTier(tier)
	if !ok {
		return nil, ErrInvalidTTL
	}
	now := time.Now()
	link := models.GateShareLink{
		ID:               newShareID(),
		TokenHash:        HashToken(token),
		Kind:             models.ShareLinkKindReview,
		RunID:            runID,
		NodeID:           nodeID,
		Iteration:        conv.Iteration,
		CreatedBy:        strings.TrimSpace(createdBy),
		TTLTier:          tier,
		PermissionPreset: preset,
		ExpiresAt:        now.Add(dur),
		CreatedAt:        now,
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := revokeActiveInTx(tx, runID, nodeID, conv.Iteration, now, models.ShareLinkKindReview); err != nil {
			return err
		}
		return tx.Create(&link).Error
	}); err != nil {
		return nil, err
	}
	s.notifyInvalidated([]string{latest.TokenHash})
	s.recordShareAudit(run, models.Gate{NodeID: nodeID, Iteration: conv.Iteration}, models.AuditActionGateShareRegen, createdBy, map[string]any{
		"ttlTier":          tier,
		"permissionPreset": preset,
		"expiresAt":        link.ExpiresAt,
		"createdAt":        link.CreatedAt,
		"linkId":           link.ID,
		"revokedAt":        now,
		"kind":             models.ShareLinkKindReview,
	})
	return &CreateResult{
		ID:               link.ID,
		URL:              ShareURL(publicOrigin, token),
		TTLTier:          tier,
		PermissionPreset: preset,
		ExpiresAt:        link.ExpiresAt,
		State:            models.ShareLinkStateActive,
	}, nil
}

// RevokeReview immediately invalidates the active review link.
func (s *Service) RevokeReview(runID, nodeID, actor string) error {
	conv, run, err := s.currentPendingReview(runID, nodeID)
	if err != nil && err != ErrReviewNotPending && err != ErrUsedReadonly && err != ErrRunEnded {
		return err
	}
	if conv.ID == 0 {
		var c models.ReactConversation
		if e := s.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
			Order("iteration desc, id desc").First(&c).Error; e != nil {
			return ErrNotFound
		}
		conv = c
	}
	latest, err := s.latestLink(runID, nodeID, conv.Iteration, models.ShareLinkKindReview)
	if err != nil {
		return err
	}
	if latest == nil {
		return ErrNotFound
	}
	if linkState(latest, time.Now()) != models.ShareLinkStateActive {
		return ErrNotActive
	}
	now := time.Now()
	if err := s.db.Model(&models.GateShareLink{}).Where("id = ?", latest.ID).
		Update("revoked_at", now).Error; err != nil {
		return err
	}
	s.notifyInvalidated([]string{latest.TokenHash})
	s.recordShareAudit(run, models.Gate{NodeID: nodeID, Iteration: conv.Iteration}, models.AuditActionGateShareRevoke, actor, map[string]any{
		"revokedAt":        now,
		"expiresAt":        latest.ExpiresAt,
		"createdAt":        latest.CreatedAt,
		"linkId":           latest.ID,
		"ttlTier":          latest.TTLTier,
		"permissionPreset": latest.PermissionPreset,
		"kind":             models.ShareLinkKindReview,
	})
	return nil
}
