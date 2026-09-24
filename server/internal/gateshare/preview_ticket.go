package gateshare

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

const (
	// Preview tickets are short-lived so they may appear in WS query strings
	// without matching the long-lived share-token prohibition in SECURITY.md.
	previewTicketTTL      = 2 * time.Minute
	maxTicketsPerLink     = 8
	PreviewPurposeVNC     = "vnc"
	PreviewPurposeAPI     = "api"
	previewTicketHexBytes = 16
)

// PreviewTicketClaims is the server-side resolution of a public preview ticket.
type PreviewTicketClaims struct {
	TokenHash string
	RunID     string
	NodeID    string
	Port      int
	Purpose   string
	ExpiresAt time.Time
}

// TicketStore issues short-lived preview tickets keyed by share-link token hash.
type TicketStore struct {
	db *gorm.DB
}

// NewTicketStore builds a DB-backed preview ticket store.
func NewTicketStore(db *gorm.DB) *TicketStore {
	return &TicketStore{db: db}
}

// Issue creates a short-lived ticket bound to tokenHash + run/node/port/purpose.
func (s *TicketStore) Issue(tokenHash, runID, nodeID string, port int, purpose string) (string, time.Time, error) {
	if s == nil || s.db == nil {
		return "", time.Time{}, errors.New("ticket store unavailable")
	}
	tokenHash = strings.TrimSpace(tokenHash)
	runID = strings.TrimSpace(runID)
	nodeID = strings.TrimSpace(nodeID)
	purpose = normalizePreviewPurpose(purpose)
	if tokenHash == "" || runID == "" || nodeID == "" || port <= 0 || purpose == "" {
		return "", time.Time{}, errors.New("invalid ticket claims")
	}
	buf := make([]byte, previewTicketHexBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	ticket := hex.EncodeToString(buf)
	now := time.Now()
	exp := now.Add(previewTicketTTL)
	row := models.GateSharePreviewTicket{
		TokenHash: tokenHash,
		Ticket:    ticket,
		RunID:     runID,
		NodeID:    nodeID,
		Port:      port,
		Purpose:   purpose,
		ExpiresAt: exp,
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("token_hash = ? AND expires_at <= ?", tokenHash, now).
			Delete(&models.GateSharePreviewTicket{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		var alive []models.GateSharePreviewTicket
		if err := tx.Where("token_hash = ?", tokenHash).Order("id ASC").Find(&alive).Error; err != nil {
			return err
		}
		if len(alive) <= maxTicketsPerLink {
			return nil
		}
		drop := alive[:len(alive)-maxTicketsPerLink]
		ids := make([]uint, len(drop))
		for i, r := range drop {
			ids[i] = r.ID
		}
		return tx.Where("id IN ?", ids).Delete(&models.GateSharePreviewTicket{}).Error
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return ticket, exp, nil
}

// Lookup validates an unexpired ticket and returns its claims (ticket is kept
// for refresh / reconnect within TTL; handshake does not consume it).
func (s *TicketStore) Lookup(ticket string) (*PreviewTicketClaims, bool) {
	if s == nil || s.db == nil {
		return nil, false
	}
	ticket = strings.TrimSpace(ticket)
	if ticket == "" || len(ticket) != previewTicketHexBytes*2 {
		return nil, false
	}
	now := time.Now()
	var row models.GateSharePreviewTicket
	if err := s.db.Where("ticket = ? AND expires_at > ?", ticket, now).First(&row).Error; err != nil {
		_ = s.db.Where("expires_at <= ?", now).Delete(&models.GateSharePreviewTicket{}).Error
		return nil, false
	}
	// Defense in depth: constant-time compare even though DB equality matched.
	got := []byte(row.Ticket)
	want := []byte(ticket)
	if len(got) != len(want) || subtle.ConstantTimeCompare(got, want) != 1 {
		return nil, false
	}
	return &PreviewTicketClaims{
		TokenHash: row.TokenHash,
		RunID:     row.RunID,
		NodeID:    row.NodeID,
		Port:      row.Port,
		Purpose:   row.Purpose,
		ExpiresAt: row.ExpiresAt,
	}, true
}

// InvalidateByTokenHash deletes all tickets for a share-link token hash.
func (s *TicketStore) InvalidateByTokenHash(tokenHash string) {
	if s == nil || s.db == nil || strings.TrimSpace(tokenHash) == "" {
		return
	}
	_ = s.db.Where("token_hash = ?", tokenHash).Delete(&models.GateSharePreviewTicket{}).Error
}

// InvalidateByRun deletes tickets whose run_id matches.
func (s *TicketStore) InvalidateByRun(runID string) {
	if s == nil || s.db == nil || strings.TrimSpace(runID) == "" {
		return
	}
	_ = s.db.Where("run_id = ?", runID).Delete(&models.GateSharePreviewTicket{}).Error
}

// InvalidateByRunNode deletes tickets for a run/node pair.
func (s *TicketStore) InvalidateByRunNode(runID, nodeID string) {
	if s == nil || s.db == nil {
		return
	}
	runID, nodeID = strings.TrimSpace(runID), strings.TrimSpace(nodeID)
	if runID == "" || nodeID == "" {
		return
	}
	_ = s.db.Where("run_id = ? AND node_id = ?", runID, nodeID).
		Delete(&models.GateSharePreviewTicket{}).Error
}

func normalizePreviewPurpose(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case PreviewPurposeVNC:
		return PreviewPurposeVNC
	case PreviewPurposeAPI:
		return PreviewPurposeAPI
	default:
		return ""
	}
}

// InferPreviewMode is always remote desktop. Port-direct iframes are chosen by
// the node switch (directUrl), not by the port label.
func InferPreviewMode(label string) string {
	return PreviewPurposeVNC
}
