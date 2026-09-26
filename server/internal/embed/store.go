// Package embed issues the credentials behind the preview-page chat drawer.
//
// A Grasp page mints a one-shot ticket; the preview tab carries it in its URL
// fragment; the drawer (served from the Grasp origin) redeems it for a bearer
// session bound to one run/node. Tickets are visible to the preview app, so
// they are short-lived, single-use, and only redeemable from the Grasp origin.
package embed

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

const (
	TicketTTL  = 60 * time.Second
	SessionTTL = 12 * time.Hour

	// SessionPrefix marks drawer bearer tokens so share-link handlers can tell
	// them apart from share tokens by shape alone.
	SessionPrefix = "gse_"

	secretBytes = 16
)

var (
	ErrInvalid = errors.New("invalid embed claims")
	// ErrTicketSpent covers unknown, expired and already redeemed tickets.
	ErrTicketSpent = errors.New("embed ticket spent")
)

// Claims is what a ticket or session is bound to.
type Claims struct {
	Kind           string
	RunID          string
	NodeID         string
	Username       string
	ShareTokenHash string
	GraspOrigin    string
	ExpiresAt      time.Time
}

type Store struct {
	db  *gorm.DB
	now func() time.Time
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db, now: time.Now}
}

func hashSecret(s string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(s)))
	return hex.EncodeToString(sum[:])
}

func newSecret() (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// ValidTicketShape reports whether s looks like a ticket (32 hex chars).
func ValidTicketShape(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != secretBytes*2 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// IsSessionToken reports whether s has the drawer bearer token shape.
func IsSessionToken(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, SessionPrefix) && ValidTicketShape(strings.TrimPrefix(s, SessionPrefix))
}

func validClaims(c Claims) bool {
	if strings.TrimSpace(c.RunID) == "" || strings.TrimSpace(c.NodeID) == "" {
		return false
	}
	switch c.Kind {
	case models.EmbedKindSession:
		return strings.TrimSpace(c.Username) != ""
	case models.EmbedKindShare:
		return strings.TrimSpace(c.ShareTokenHash) != ""
	default:
		return false
	}
}

// IssueTicket mints a one-shot ticket. GraspOrigin is the browser-facing
// origin the drawer must be loaded from.
func (s *Store) IssueTicket(c Claims) (string, time.Time, error) {
	if s == nil || s.db == nil {
		return "", time.Time{}, errors.New("embed store unavailable")
	}
	if !validClaims(c) || strings.TrimSpace(c.GraspOrigin) == "" {
		return "", time.Time{}, ErrInvalid
	}
	ticket, err := newSecret()
	if err != nil {
		return "", time.Time{}, err
	}
	now := s.now()
	exp := now.Add(TicketTTL)
	row := models.EmbedTicket{
		TicketHash:     hashSecret(ticket),
		Kind:           c.Kind,
		RunID:          strings.TrimSpace(c.RunID),
		NodeID:         strings.TrimSpace(c.NodeID),
		Username:       strings.TrimSpace(c.Username),
		ShareTokenHash: strings.TrimSpace(c.ShareTokenHash),
		GraspOrigin:    strings.TrimRight(strings.TrimSpace(c.GraspOrigin), "/"),
		ExpiresAt:      exp,
	}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at <= ?", now).Delete(&models.EmbedTicket{}).Error; err != nil {
			return err
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return ticket, exp, nil
}

func ticketClaims(row models.EmbedTicket) *Claims {
	return &Claims{
		Kind:           row.Kind,
		RunID:          row.RunID,
		NodeID:         row.NodeID,
		Username:       row.Username,
		ShareTokenHash: row.ShareTokenHash,
		GraspOrigin:    row.GraspOrigin,
		ExpiresAt:      row.ExpiresAt,
	}
}

// PeekTicket returns the claims of an unexpired, unconsumed ticket without
// consuming it. The sandbox uses it to learn which origin serves the drawer.
func (s *Store) PeekTicket(ticket string) (*Claims, bool) {
	if s == nil || s.db == nil || !ValidTicketShape(ticket) {
		return nil, false
	}
	var row models.EmbedTicket
	err := s.db.Where("ticket_hash = ? AND consumed_at IS NULL AND expires_at > ?", hashSecret(ticket), s.now()).
		First(&row).Error
	if err != nil {
		return nil, false
	}
	return ticketClaims(row), true
}

func redeemTicketTx(tx *gorm.DB, ticket string, now time.Time) (*Claims, error) {
	h := hashSecret(ticket)
	res := tx.Model(&models.EmbedTicket{}).
		Where("ticket_hash = ? AND consumed_at IS NULL AND expires_at > ?", h, now).
		Update("consumed_at", now)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected != 1 {
		return nil, ErrTicketSpent
	}
	var row models.EmbedTicket
	if err := tx.Where("ticket_hash = ?", h).First(&row).Error; err != nil {
		return nil, err
	}
	return ticketClaims(row), nil
}

// ExchangeTicket redeems a ticket and mints its drawer token in one
// transaction, so a failed insert leaves the ticket redeemable.
func (s *Store) ExchangeTicket(ticket string) (*Claims, string, time.Time, error) {
	if s == nil || s.db == nil {
		return nil, "", time.Time{}, errors.New("embed store unavailable")
	}
	if !ValidTicketShape(ticket) {
		return nil, "", time.Time{}, ErrTicketSpent
	}
	var (
		claims *Claims
		token  string
		exp    time.Time
	)
	now := s.now()
	err := s.db.Transaction(func(tx *gorm.DB) error {
		c, err := redeemTicketTx(tx, ticket, now)
		if err != nil {
			return err
		}
		claims = c
		token, exp, err = createSessionTx(tx, *c, now)
		return err
	})
	if err != nil {
		return nil, "", time.Time{}, err
	}
	return claims, token, exp, nil
}

// CreateSession mints a drawer bearer token for already-validated claims.
func (s *Store) CreateSession(c Claims) (string, time.Time, error) {
	if s == nil || s.db == nil {
		return "", time.Time{}, errors.New("embed store unavailable")
	}
	var (
		token string
		exp   time.Time
	)
	now := s.now()
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		token, exp, err = createSessionTx(tx, c, now)
		return err
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return token, exp, nil
}

func createSessionTx(tx *gorm.DB, c Claims, now time.Time) (string, time.Time, error) {
	if !validClaims(c) {
		return "", time.Time{}, ErrInvalid
	}
	secret, err := newSecret()
	if err != nil {
		return "", time.Time{}, err
	}
	token := SessionPrefix + secret
	exp := now.Add(SessionTTL)
	row := models.EmbedSession{
		TokenHash:      hashSecret(token),
		Kind:           c.Kind,
		RunID:          strings.TrimSpace(c.RunID),
		NodeID:         strings.TrimSpace(c.NodeID),
		Username:       strings.TrimSpace(c.Username),
		ShareTokenHash: strings.TrimSpace(c.ShareTokenHash),
		ExpiresAt:      exp,
	}
	if err := tx.Where("expires_at <= ?", now).Delete(&models.EmbedSession{}).Error; err != nil {
		return "", time.Time{}, err
	}
	if err := tx.Create(&row).Error; err != nil {
		return "", time.Time{}, err
	}
	return token, exp, nil
}

// LookupSession resolves an unexpired drawer bearer token.
func (s *Store) LookupSession(token string) (*Claims, bool) {
	if s == nil || s.db == nil || !IsSessionToken(token) {
		return nil, false
	}
	var row models.EmbedSession
	err := s.db.Where("token_hash = ? AND expires_at > ?", hashSecret(token), s.now()).First(&row).Error
	if err != nil {
		return nil, false
	}
	return &Claims{
		Kind:           row.Kind,
		RunID:          row.RunID,
		NodeID:         row.NodeID,
		Username:       row.Username,
		ShareTokenHash: row.ShareTokenHash,
		ExpiresAt:      row.ExpiresAt,
	}, true
}

// InvalidateRun drops every ticket and session for a run.
func (s *Store) InvalidateRun(runID string) {
	if s == nil || s.db == nil || strings.TrimSpace(runID) == "" {
		return
	}
	_ = s.db.Where("run_id = ?", runID).Delete(&models.EmbedTicket{}).Error
	_ = s.db.Where("run_id = ?", runID).Delete(&models.EmbedSession{}).Error
}

// InvalidateShare drops sessions derived from a share link token hash.
func (s *Store) InvalidateShare(tokenHashes ...string) {
	if s == nil || s.db == nil || len(tokenHashes) == 0 {
		return
	}
	_ = s.db.Where("share_token_hash IN ?", tokenHashes).Delete(&models.EmbedTicket{}).Error
	_ = s.db.Where("share_token_hash IN ?", tokenHashes).Delete(&models.EmbedSession{}).Error
}
