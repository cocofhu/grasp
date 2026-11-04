package services

import (
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

// CodexLoginHooks receives Codex login lifecycle changes made through the
// credential service: a saved login is pushed to running sandboxes, a gone one
// is no longer handed out. Info decorates the credential list.
type CodexLoginHooks interface {
	CodexLoginSaved(credentialID string)
	CodexLoginGone(credentialID string)
	CodexLoginInfo(credentialID string) *CodexLoginInfo
}

// CodexLoginInfo is the token-free login state shown on the credentials page.
type CodexLoginInfo struct {
	Email       string     `json:"email,omitempty"`
	LastRefresh *time.Time `json:"lastRefresh,omitempty"`
	Status      string     `json:"status"`
}

// SetCodexLoginHooks wires the Codex login manager.
func (s *CredentialService) SetCodexLoginHooks(h CodexLoginHooks) {
	s.codexHooks = h
}

// codexLoginRow reports whether row is a workspace Codex ChatGPT login.
func codexLoginRow(row models.Credential) bool {
	return currentCredential(row) && row.Purpose == "tool_auth" && row.ToolID == "codex" && row.EnvKey == envauth.EnvCodexAuthFile
}

func codexLoginUsable(row models.Credential) bool {
	return codexLoginRow(row) && row.Enabled && row.RevokedAt == nil && strings.TrimSpace(row.ValueEnc) != ""
}

// notifyCodexLogin tells the hooks whether a just-written row still holds a
// usable Codex login.
func (s *CredentialService) notifyCodexLogin(row models.Credential, secretChanged bool) {
	if s.codexHooks == nil || !codexLoginRow(row) {
		return
	}
	if !codexLoginUsable(row) {
		s.codexHooks.CodexLoginGone(row.ID)
		return
	}
	if secretChanged {
		s.codexHooks.CodexLoginSaved(row.ID)
	}
}

func (s *CredentialService) forgetCodexLogin(id string) {
	if s.codexHooks != nil {
		s.codexHooks.CodexLoginGone(strings.TrimSpace(id))
	}
}

// codexStoredLogin is one decrypted Codex login with the ciphertext and
// revision it was read at, for a compare-and-swap save.
type codexStoredLogin struct {
	ID       string
	Body     string
	ValueEnc string
	Revision uint64
}

// codexLogin loads a usable Codex login. Anything else is ErrCredentialNotFound.
func (s *CredentialService) codexLogin(id string) (codexStoredLogin, error) {
	var row models.Credential
	if err := s.db.Where("id = ? AND enabled = ? AND revoked_at IS NULL", strings.TrimSpace(id), true).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return codexStoredLogin{}, ErrCredentialNotFound
		}
		return codexStoredLogin{}, err
	}
	if !codexLoginUsable(row) {
		return codexStoredLogin{}, ErrCredentialNotFound
	}
	body, err := crypto.Decrypt(row.ValueEnc)
	if err != nil {
		return codexStoredLogin{}, err
	}
	return codexStoredLogin{ID: row.ID, Body: body, ValueEnc: row.ValueEnc, Revision: row.SecretRevision}, nil
}

// codexLoginIDs lists the usable Codex logins.
func (s *CredentialService) codexLoginIDs() ([]string, error) {
	var rows []models.Credential
	if err := s.db.Where("enabled = ? AND revoked_at IS NULL AND env_key = ?", true, envauth.EnvCodexAuthFile).Find(&rows).Error; err != nil {
		return nil, err
	}
	var out []string
	for _, row := range rows {
		if codexLoginUsable(row) {
			out = append(out, row.ID)
		}
	}
	return out, nil
}

// errCodexLoginConflict reports that the login changed since it was read (the
// user signed in again, or it was revoked): the stored value wins.
var errCodexLoginConflict = errors.New("Codex login credential changed during refresh")

// saveCodexLogin stores a renewed login only if the row still holds prev. It
// returns the new revision.
func (s *CredentialService) saveCodexLogin(prev codexStoredLogin, content string) (uint64, error) {
	enc, err := crypto.Encrypt(content)
	if err != nil {
		return 0, err
	}
	next := nextSecretRevision(prev.Revision)
	result := s.db.Model(&models.Credential{}).
		Where("id = ? AND value_enc = ? AND enabled = ? AND revoked_at IS NULL", prev.ID, prev.ValueEnc, true).
		Updates(map[string]any{"value_enc": enc, "updated_at": time.Now(), "secret_revision": next})
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, errCodexLoginConflict
	}
	return next, nil
}
