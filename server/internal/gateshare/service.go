package gateshare

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/embed"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// InboxShareStatus is the leak-free share-link chip DTO (no plaintext token).
type InboxShareStatus struct {
	State            string     `json:"state"`
	TTLTier          string     `json:"ttlTier,omitempty"`
	PermissionPreset string     `json:"permissionPreset,omitempty"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	RemainingSec     *int64     `json:"remainingSec,omitempty"`
	UsedAt           *time.Time `json:"usedAt,omitempty"`
	RevokedAt        *time.Time `json:"revokedAt,omitempty"`
	CanCreate        bool       `json:"canCreate"`
	CanManage        bool       `json:"canManage"`
	HasPass          bool       `json:"hasPass"`
	HasFail          bool       `json:"hasFail"`
}

// CreateResult is returned only to the management surface (may include fragment URL).
type CreateResult struct {
	ID               string    `json:"id"`
	URL              string    `json:"url"`
	TTLTier          string    `json:"ttlTier"`
	PermissionPreset string    `json:"permissionPreset"`
	ExpiresAt        time.Time `json:"expiresAt"`
	State            string    `json:"state"`
}

// LookupResult is a validated share link plus its bound instance (no plaintext token).
// Gate is populated for human_gate; review rows leave Gate zero-value.
type LookupResult struct {
	Link models.GateShareLink
	Kind string
	Gate models.Gate
	Run  models.Run
	Node *models.Node
}

func normalizeShareKind(kind string) string {
	if strings.TrimSpace(kind) == models.ShareLinkKindReview {
		return models.ShareLinkKindReview
	}
	return models.ShareLinkKindHumanGate
}

func gateIDPtr(id uint) *uint {
	if id == 0 {
		return nil
	}
	v := id
	return &v
}

// LinkInvalidationHook is notified with token hashes when share links are
// revoked/consumed so preview tickets and live sessions can be cut.
type LinkInvalidationHook func(tokenHashes []string)

// Service manages GateShareLink lifecycle.
type Service struct {
	db           *gorm.DB
	audit        *services.ProjectAuditService
	onInvalidate LinkInvalidationHook
	embedLookup  EmbedLookup
}

// EmbedRef is what a chat-drawer bearer token stands for: the share link it
// was minted from, or (logged-in drawer) the run/node review session itself.
type EmbedRef struct {
	ShareTokenHash string
	RunID          string
	NodeID         string
	ExpiresAt      time.Time
}

// EmbedLookup resolves a chat-drawer bearer token.
type EmbedLookup func(token string) (EmbedRef, bool)

// SetEmbedLookup lets LookupByToken accept drawer bearer tokens.
func (s *Service) SetEmbedLookup(f EmbedLookup) {
	if s == nil {
		return
	}
	s.embedLookup = f
}

// ValidCredentialShape accepts a share token or a drawer bearer token. Only
// chat endpoints use it; decide / ticket endpoints keep ValidTokenShape so a
// drawer token can never approve or mint further credentials.
func ValidCredentialShape(s string) bool {
	return ValidTokenShape(s) || embed.IsSessionToken(s)
}

// NewService builds the share-link service.
func NewService(db *gorm.DB, audit *services.ProjectAuditService) *Service {
	return &Service{db: db, audit: audit}
}

// SetInvalidationHook registers a callback invoked after links are revoked.
func (s *Service) SetInvalidationHook(h LinkInvalidationHook) {
	if s == nil {
		return
	}
	s.onInvalidate = h
}

func (s *Service) notifyInvalidated(tokenHashes []string) {
	if s == nil || s.onInvalidate == nil || len(tokenHashes) == 0 {
		return
	}
	s.onInvalidate(tokenHashes)
}

func (s *Service) pluckTokenHashes(where string, args ...any) []string {
	if s == nil || s.db == nil {
		return nil
	}
	var hashes []string
	_ = s.db.Model(&models.GateShareLink{}).Where(where, args...).Pluck("token_hash", &hashes).Error
	return hashes
}

func newShareID() string {
	return "gsl-" + uuid.NewString()[:12]
}

func terminalRun(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func linkState(link *models.GateShareLink, now time.Time) string {
	if link == nil || link.ID == "" {
		return models.ShareLinkStateNone
	}
	if link.UsedAt != nil {
		return models.ShareLinkStateUsed
	}
	if link.RevokedAt != nil {
		return models.ShareLinkStateRevoked
	}
	if !link.ExpiresAt.After(now) {
		return models.ShareLinkStateExpired
	}
	return models.ShareLinkStateActive
}

func (s *Service) latestLink(runID, nodeID string, iteration int, kind string) (*models.GateShareLink, error) {
	var link models.GateShareLink
	q := s.db.Where("run_id = ? AND node_id = ? AND iteration = ?", runID, nodeID, iteration)
	if k := strings.TrimSpace(kind); k != "" {
		q = q.Where("kind = ?", normalizeShareKind(k))
	}
	err := q.Order("created_at desc, id desc").First(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &link, nil
}

func (s *Service) currentPendingGate(runID, nodeID string) (models.Gate, models.Run, error) {
	var run models.Run
	if err := s.db.First(&run, "id = ?", runID).Error; err != nil {
		return models.Gate{}, models.Run{}, ErrGateNotPending
	}
	if terminalRun(run.Status) {
		return models.Gate{}, run, ErrRunEnded
	}
	var gate models.Gate
	if err := s.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Order("iteration desc, id desc").First(&gate).Error; err != nil {
		return models.Gate{}, run, ErrGateNotPending
	}
	if gate.Resolved {
		return gate, run, ErrGateNotPending
	}
	node := run.Graph.FindNode(nodeID)
	if node == nil || node.Type != "human_gate" {
		return gate, run, ErrNotHumanGate
	}
	return gate, run, nil
}

// Status returns leak-free status for a human_gate instance.
func (s *Service) Status(runID, nodeID string) (InboxShareStatus, error) {
	st := InboxShareStatus{State: models.ShareLinkStateNone}
	gate, run, err := s.currentPendingGate(runID, nodeID)
	if err == ErrNotHumanGate {
		return st, err
	}
	if err != nil && gate.ID == 0 {
		var g models.Gate
		if e := s.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
			Order("iteration desc, id desc").First(&g).Error; e != nil {
			return st, ErrGateNotPending
		}
		gate = g
		_ = s.db.First(&run, "id = ?", runID)
	}
	link, lerr := s.latestLink(runID, nodeID, gate.Iteration, models.ShareLinkKindHumanGate)
	if lerr != nil {
		return st, lerr
	}
	st = s.statusFrom(link, gate, run, time.Now())
	if err == ErrNotHumanGate {
		return st, err
	}
	return st, nil
}

func (s *Service) statusFrom(link *models.GateShareLink, gate models.Gate, run models.Run, now time.Time) InboxShareStatus {
	st := InboxShareStatus{
		State:   linkState(link, now),
		HasPass: ResolvePassAction(gate.Actions) != "",
		HasFail: ResolveFailAction(gate.Actions) != "",
	}
	if link != nil {
		st.TTLTier = link.TTLTier
		exp := link.ExpiresAt
		st.ExpiresAt = &exp
		st.UsedAt = link.UsedAt
		st.RevokedAt = link.RevokedAt
		if st.State == models.ShareLinkStateActive {
			st.PermissionPreset = NormalizePermissionPreset(link.PermissionPreset)
			rem := int64(time.Until(link.ExpiresAt).Seconds())
			if rem < 0 {
				rem = 0
			}
			st.RemainingSec = &rem
		}
	}
	pending := !gate.Resolved && !terminalRun(run.Status)
	hasStd := HasStandardAction(gate.Actions)
	switch st.State {
	case models.ShareLinkStateNone, models.ShareLinkStateRevoked, models.ShareLinkStateExpired:
		st.CanCreate = pending && hasStd
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

// Create mints a new active link (revoking any prior active for the instance).
func (s *Service) Create(runID, nodeID, ttlTier, permissionPreset, createdBy, publicOrigin string) (*CreateResult, error) {
	gate, run, err := s.currentPendingGate(runID, nodeID)
	if err != nil {
		if gate.ID != 0 && gate.Resolved {
			return nil, ErrUsedReadonly
		}
		return nil, err
	}
	if !HasStandardAction(gate.Actions) {
		return nil, ErrNoStandardAction
	}
	tier, dur, ok := ParseTTLTier(ttlTier)
	if !ok {
		return nil, ErrInvalidTTL
	}
	preset, ok := ParsePermissionPreset(permissionPreset)
	if !ok {
		return nil, ErrInvalidPermissionPreset
	}
	latest, err := s.latestLink(runID, nodeID, gate.Iteration, models.ShareLinkKindHumanGate)
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
		Kind:             models.ShareLinkKindHumanGate,
		RunID:            runID,
		NodeID:           nodeID,
		Iteration:        gate.Iteration,
		GateID:           gateIDPtr(gate.ID),
		CreatedBy:        strings.TrimSpace(createdBy),
		TTLTier:          tier,
		PermissionPreset: preset,
		ExpiresAt:        now.Add(dur),
		CreatedAt:        now,
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := revokeActiveInTx(tx, runID, nodeID, gate.Iteration, now, models.ShareLinkKindHumanGate); err != nil {
			return err
		}
		return tx.Create(&link).Error
	}); err != nil {
		return nil, err
	}
	s.recordShareAudit(run, gate, models.AuditActionGateShareCreate, createdBy, map[string]any{
		"ttlTier":          tier,
		"permissionPreset": preset,
		"expiresAt":        link.ExpiresAt,
		"createdAt":        link.CreatedAt,
		"linkId":           link.ID,
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

// Regenerate immediately revokes the active link and mints a new one with the same TTL tier.
func (s *Service) Regenerate(runID, nodeID, createdBy, publicOrigin string) (*CreateResult, error) {
	gate, run, err := s.currentPendingGate(runID, nodeID)
	if err != nil {
		return nil, err
	}
	latest, err := s.latestLink(runID, nodeID, gate.Iteration, models.ShareLinkKindHumanGate)
	if err != nil {
		return nil, err
	}
	if latest == nil || linkState(latest, time.Now()) != models.ShareLinkStateActive {
		return nil, ErrNotActive
	}
	tier := latest.TTLTier
	preset := NormalizePermissionPreset(latest.PermissionPreset)
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
		Kind:             models.ShareLinkKindHumanGate,
		RunID:            runID,
		NodeID:           nodeID,
		Iteration:        gate.Iteration,
		GateID:           gateIDPtr(gate.ID),
		CreatedBy:        strings.TrimSpace(createdBy),
		TTLTier:          tier,
		PermissionPreset: preset,
		ExpiresAt:        now.Add(dur),
		CreatedAt:        now,
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := revokeActiveInTx(tx, runID, nodeID, gate.Iteration, now, models.ShareLinkKindHumanGate); err != nil {
			return err
		}
		return tx.Create(&link).Error
	}); err != nil {
		return nil, err
	}
	s.notifyInvalidated([]string{latest.TokenHash})
	s.recordShareAudit(run, gate, models.AuditActionGateShareRegen, createdBy, map[string]any{
		"ttlTier":          tier,
		"permissionPreset": preset,
		"expiresAt":        link.ExpiresAt,
		"createdAt":        link.CreatedAt,
		"linkId":           link.ID,
		"revokedAt":        now,
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

// Revoke immediately invalidates the active link.
func (s *Service) Revoke(runID, nodeID, actor string) error {
	gate, run, err := s.currentPendingGate(runID, nodeID)
	if err != nil && err != ErrGateNotPending && err != ErrUsedReadonly && err != ErrRunEnded {
		return err
	}
	if gate.ID == 0 {
		var g models.Gate
		if e := s.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
			Order("iteration desc, id desc").First(&g).Error; e != nil {
			return ErrNotFound
		}
		gate = g
	}
	latest, err := s.latestLink(runID, nodeID, gate.Iteration, models.ShareLinkKindHumanGate)
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
	s.recordShareAudit(run, gate, models.AuditActionGateShareRevoke, actor, map[string]any{
		"revokedAt":        now,
		"expiresAt":        latest.ExpiresAt,
		"createdAt":        latest.CreatedAt,
		"linkId":           latest.ID,
		"ttlTier":          latest.TTLTier,
		"permissionPreset": NormalizePermissionPreset(latest.PermissionPreset),
	})
	return nil
}

func revokeActiveInTx(tx *gorm.DB, runID, nodeID string, iteration int, now time.Time, kind string) error {
	q := tx.Model(&models.GateShareLink{}).
		Where("run_id = ? AND node_id = ? AND iteration = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?",
			runID, nodeID, iteration, now)
	if k := strings.TrimSpace(kind); k != "" {
		q = q.Where("kind = ?", normalizeShareKind(k))
	}
	return q.Update("revoked_at", now).Error
}

// RevokeUnusedForRun invalidates unused active links for a run (cancel/finish).
func (s *Service) RevokeUnusedForRun(runID string) {
	if s == nil || s.db == nil || strings.TrimSpace(runID) == "" {
		return
	}
	now := time.Now()
	where := "run_id = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?"
	hashes := s.pluckTokenHashes(where, runID, now)
	_ = s.db.Model(&models.GateShareLink{}).Where(where, runID, now).Update("revoked_at", now).Error
	s.notifyInvalidated(hashes)
}

// RevokeUnusedForGate invalidates unused active links for one gate instance.
func (s *Service) RevokeUnusedForGate(runID, nodeID string, iteration int) {
	if s == nil || s.db == nil {
		return
	}
	now := time.Now()
	where := "run_id = ? AND node_id = ? AND iteration = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?"
	hashes := s.pluckTokenHashes(where, runID, nodeID, iteration, now)
	_ = s.db.Model(&models.GateShareLink{}).Where(where, runID, nodeID, iteration, now).Update("revoked_at", now).Error
	s.notifyInvalidated(hashes)
}

// RevokeUnusedForNode invalidates unused active links for a node (any iteration).
func (s *Service) RevokeUnusedForNode(runID, nodeID string) {
	if s == nil || s.db == nil {
		return
	}
	now := time.Now()
	where := "run_id = ? AND node_id = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?"
	hashes := s.pluckTokenHashes(where, runID, nodeID, now)
	_ = s.db.Model(&models.GateShareLink{}).Where(where, runID, nodeID, now).Update("revoked_at", now).Error
	s.notifyInvalidated(hashes)
}

// LookupByToken hashes the token, loads by unique hash, then constant-time compares.
// A drawer bearer token resolves through SetEmbedLookup to the same link.
func (s *Service) LookupByToken(token string) (*LookupResult, string, error) {
	var hash string
	switch {
	case ValidTokenShape(token):
		hash = HashToken(token)
	case embed.IsSessionToken(token) && s.embedLookup != nil:
		ref, ok := s.embedLookup(token)
		if !ok {
			return nil, models.ShareLinkStateNone, ErrTokenInvalid
		}
		if ref.ShareTokenHash == "" {
			return s.lookupRunNodeReview(ref, HashToken(token))
		}
		hash = ref.ShareTokenHash
	default:
		return nil, models.ShareLinkStateNone, ErrTokenInvalid
	}
	var link models.GateShareLink
	if err := s.db.Where("token_hash = ?", hash).First(&link).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, models.ShareLinkStateNone, ErrTokenInvalid
		}
		return nil, models.ShareLinkStateNone, err
	}
	if !EqualHash(link.TokenHash, hash) {
		return nil, models.ShareLinkStateNone, ErrTokenInvalid
	}
	st := linkState(&link, time.Now())
	kind := normalizeShareKind(link.Kind)
	var run models.Run
	if err := s.db.First(&run, "id = ?", link.RunID).Error; err != nil {
		return nil, st, ErrTokenInvalid
	}
	res := &LookupResult{Link: link, Kind: kind, Run: run, Node: run.Graph.FindNode(link.NodeID)}
	if kind == models.ShareLinkKindReview {
		if st == models.ShareLinkStateActive {
			if terminalRun(run.Status) {
				st = models.ShareLinkStateUsed
			} else {
				var conv models.ReactConversation
				if err := s.db.Where("run_id = ? AND node_id = ? AND iteration = ?", link.RunID, link.NodeID, link.Iteration).
					First(&conv).Error; err != nil || conv.Done || run.Status != "waiting_human" {
					st = models.ShareLinkStateUsed
				} else {
					var latest models.ReactConversation
					if err := s.db.Where("run_id = ? AND node_id = ?", link.RunID, link.NodeID).
						Order("iteration desc, id desc").First(&latest).Error; err == nil {
						if latest.Iteration != link.Iteration {
							st = models.ShareLinkStateRevoked
						}
					}
				}
			}
		}
		return res, st, nil
	}
	if link.GateID == nil || *link.GateID == 0 {
		return nil, st, ErrTokenInvalid
	}
	var gate models.Gate
	if err := s.db.First(&gate, "id = ?", *link.GateID).Error; err != nil {
		return nil, st, ErrTokenInvalid
	}
	res.Gate = gate
	if st == models.ShareLinkStateActive {
		if terminalRun(run.Status) || gate.Resolved {
			st = models.ShareLinkStateUsed
		} else {
			var latest models.Gate
			if err := s.db.Where("run_id = ? AND node_id = ?", link.RunID, link.NodeID).
				Order("iteration desc, id desc").First(&latest).Error; err == nil {
				if latest.ID != gate.ID || latest.Iteration != link.Iteration {
					st = models.ShareLinkStateRevoked
				}
			}
		}
	}
	return res, st, nil
}

// lookupRunNodeReview backs a logged-in drawer token with a review link that
// exists only in memory: full reply permission, the latest conversation, and
// the same demotion rules as a stored review link. It has no ID, so it can
// never be consumed by decide.
func (s *Service) lookupRunNodeReview(ref EmbedRef, tokenHash string) (*LookupResult, string, error) {
	var run models.Run
	if err := s.db.First(&run, "id = ?", ref.RunID).Error; err != nil {
		return nil, models.ShareLinkStateNone, ErrTokenInvalid
	}
	node := run.Graph.FindNode(ref.NodeID)
	if !services.IsShareableReviewSession(node) {
		return nil, models.ShareLinkStateNone, ErrTokenInvalid
	}
	var conv models.ReactConversation
	if err := s.db.Where("run_id = ? AND node_id = ?", ref.RunID, ref.NodeID).
		Order("iteration desc, id desc").First(&conv).Error; err != nil {
		return nil, models.ShareLinkStateNone, ErrTokenInvalid
	}
	link := models.GateShareLink{
		RunID:            ref.RunID,
		NodeID:           ref.NodeID,
		Iteration:        conv.Iteration,
		Kind:             models.ShareLinkKindReview,
		PermissionPreset: models.SharePermissionFull,
		TokenHash:        tokenHash,
		ExpiresAt:        ref.ExpiresAt,
	}
	st := models.ShareLinkStateActive
	if terminalRun(run.Status) || conv.Done || run.Status != "waiting_human" {
		st = models.ShareLinkStateUsed
	}
	return &LookupResult{Link: link, Kind: models.ShareLinkKindReview, Run: run, Node: node}, st, nil
}

// ConsumeCAS marks the link used iff still active. RowsAffected==0 → already consumed/revoked/expired.
func (s *Service) ConsumeCAS(linkID, action string) (bool, *models.GateShareLink, error) {
	now := time.Now()
	res := s.db.Model(&models.GateShareLink{}).
		Where("id = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?", linkID, now).
		Updates(map[string]any{"used_at": now, "used_action": action})
	if res.Error != nil {
		return false, nil, res.Error
	}
	var link models.GateShareLink
	if err := s.db.First(&link, "id = ?", linkID).Error; err != nil {
		return false, nil, err
	}
	if res.RowsAffected == 1 {
		s.notifyInvalidated([]string{link.TokenHash})
	}
	return res.RowsAffected == 1, &link, nil
}

// RollbackConsume clears used_at/used_action so a failed resume can retry.
func (s *Service) RollbackConsume(linkID string) error {
	if s == nil || s.db == nil || strings.TrimSpace(linkID) == "" {
		return errors.New("share service unavailable")
	}
	res := s.db.Model(&models.GateShareLink{}).
		Where("id = ? AND used_at IS NOT NULL AND revoked_at IS NULL", linkID).
		Updates(map[string]any{
			"used_at":     gorm.Expr("NULL"),
			"used_action": "",
		})
	return res.Error
}

// LoadLinkByTokenHash loads a share link by its stored token hash.
func (s *Service) LoadLinkByTokenHash(tokenHash string) (*models.GateShareLink, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("share service unavailable")
	}
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return nil, ErrNotFound
	}
	var link models.GateShareLink
	if err := s.db.Where("token_hash = ?", tokenHash).First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

// LoadLinkByID loads a share link row.
func (s *Service) LoadLinkByID(id string) (*models.GateShareLink, error) {
	var link models.GateShareLink
	if err := s.db.First(&link, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

// AttachInboxStatus fills shareLink on gate, inbox-review, app_preview, and
// clarify (react) items (no plaintext token).
func (s *Service) AttachInboxStatus(items []any) {
	if s == nil || s.db == nil || len(items) == 0 {
		return
	}
	type key struct {
		run  string
		node string
		iter int
	}
	keys := make([]key, 0)
	seen := map[key]struct{}{}
	for _, it := range items {
		switch v := it.(type) {
		case services.GateInboxItem:
			if v.Type != "gate" {
				continue
			}
			k := key{v.RunID, v.NodeID, v.Iteration}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			keys = append(keys, k)
		case services.ClarifyInboxItem:
			if v.Kind != "review" && v.Kind != "app_preview" && v.Kind != "clarify" {
				continue
			}
			// Sandbox still booting: there is no session to share yet.
			if v.State == "starting" {
				continue
			}
			k := key{v.RunID, v.NodeID, v.Iteration}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return
	}
	runIDs := make([]string, 0, len(keys))
	runSeen := map[string]struct{}{}
	for _, k := range keys {
		if _, ok := runSeen[k.run]; ok {
			continue
		}
		runSeen[k.run] = struct{}{}
		runIDs = append(runIDs, k.run)
	}
	var links []models.GateShareLink
	s.db.Where("run_id IN ?", runIDs).Order("created_at desc").Find(&links)
	latest := map[key]*models.GateShareLink{}
	for i := range links {
		k := key{links[i].RunID, links[i].NodeID, links[i].Iteration}
		if _, ok := latest[k]; !ok {
			latest[k] = &links[i]
		}
	}
	var runs []models.Run
	s.db.Where("id IN ?", runIDs).Find(&runs)
	runByID := map[string]models.Run{}
	for _, r := range runs {
		runByID[r.ID] = r
	}
	now := time.Now()
	for i, it := range items {
		switch v := it.(type) {
		case services.GateInboxItem:
			k := key{v.RunID, v.NodeID, v.Iteration}
			link := latest[k]
			run := runByID[v.RunID]
			gate := models.Gate{
				RunID: v.RunID, NodeID: v.NodeID, Iteration: v.Iteration,
				Actions: v.Actions, Resolved: false,
			}
			st := s.statusFrom(link, gate, run, now)
			v.ShareLink = inboxStatusPtr(st)
			items[i] = v
		case services.ClarifyInboxItem:
			if v.Kind != "review" && v.Kind != "app_preview" && v.Kind != "clarify" {
				continue
			}
			if v.State == "starting" {
				continue
			}
			k := key{v.RunID, v.NodeID, v.Iteration}
			link := latest[k]
			run := runByID[v.RunID]
			st := s.statusFromReview(link, v.Done, run, now)
			v.ShareLink = inboxStatusPtr(st)
			items[i] = v
		}
	}
}

func inboxStatusPtr(st InboxShareStatus) *services.GateShareInboxStatus {
	return &services.GateShareInboxStatus{
		State:            st.State,
		TTLTier:          st.TTLTier,
		PermissionPreset: st.PermissionPreset,
		ExpiresAt:        st.ExpiresAt,
		RemainingSec:     st.RemainingSec,
		UsedAt:           st.UsedAt,
		RevokedAt:        st.RevokedAt,
		CanCreate:        st.CanCreate,
		CanManage:        st.CanManage,
		HasPass:          st.HasPass,
		HasFail:          st.HasFail,
	}
}

func (s *Service) recordShareAudit(run models.Run, gate models.Gate, action, actor string, extra map[string]any) {
	if s == nil || s.audit == nil {
		return
	}
	projectID := services.ResolveProjectIDForRun(s.db, run.ID)
	if projectID == "" {
		return
	}
	payload := map[string]any{
		"runId":     run.ID,
		"nodeId":    gate.NodeID,
		"iteration": gate.Iteration,
	}
	for k, v := range extra {
		payload[k] = v
	}
	s.audit.Record(services.AuditRecord{
		ProjectID:    projectID,
		Actor:        services.ActorFromUsername(actor),
		Action:       action,
		ResourceType: "gate_share",
		ResourceID:   gate.NodeID,
		RunID:        run.ID,
		NodeID:       gate.NodeID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      action,
		Payload:      payload,
	})
}

// RecordUseAudit writes gate.share.use (no plaintext token).
func (s *Service) RecordUseAudit(runID, nodeID, action, externalName, maskedIP, uaSummary string, createdAt, expiresAt time.Time, revokedAt, usedAt *time.Time) {
	if s == nil || s.audit == nil {
		return
	}
	projectID := services.ResolveProjectIDForRun(s.db, runID)
	if projectID == "" {
		return
	}
	name := strings.TrimSpace(externalName)
	display := name
	if display == "" {
		display = "外部"
	}
	payload := map[string]any{
		"action":       action,
		"externalName": name,
		"external":     true,
		"ip":           maskedIP,
		"ua":           uaSummary,
		"createdAt":    createdAt,
		"expiresAt":    expiresAt,
	}
	if revokedAt != nil {
		payload["revokedAt"] = *revokedAt
	}
	if usedAt != nil {
		payload["usedAt"] = *usedAt
	}
	s.audit.Record(services.AuditRecord{
		ProjectID:    projectID,
		Actor:        services.SystemActor(),
		CallerKind:   models.CallerKindExternal,
		Action:       models.AuditActionGateShareUse,
		ResourceType: "gate_share",
		ResourceID:   nodeID,
		RunID:        runID,
		NodeID:       nodeID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      "external " + action + " · " + display,
		Payload:      payload,
	})
}

// ShareURL builds /public/gate-approvals#t=… (token only in fragment).
func ShareURL(origin, token string) string {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin == "" {
		origin = ""
	}
	return origin + "/public/gate-approvals#t=" + token
}

// PublicOriginFromRequest reconstructs scheme+host without query/path.
func PublicOriginFromRequest(scheme, host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if scheme == "" {
		scheme = "https"
	}
	return strings.TrimRight(scheme, ":/") + "://" + host
}

// ParsePublicAdvertise returns origin (scheme://host) and host from the
// configured public_advertise URL. Empty / invalid → ("", "").
func ParsePublicAdvertise(raw string) (origin, host string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	u, err := url.Parse(raw)
	if err != nil || strings.TrimSpace(u.Host) == "" {
		return "", ""
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	if scheme != "http" && scheme != "https" {
		scheme = "https"
	}
	return scheme + "://" + u.Host, u.Host
}
