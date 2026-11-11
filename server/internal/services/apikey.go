package services

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// APIKeyService manages per-workflow external API keys.
type APIKeyService struct{ db *gorm.DB }

// NewAPIKeyService builds the service.
func NewAPIKeyService(db *gorm.DB) *APIKeyService { return &APIKeyService{db: db} }

// CreateResult holds the one-time plaintext key returned at creation.
type CreateAPIKeyResult struct {
	Key       models.WorkflowAPIKey
	Plaintext string
}

// Create generates a named key for a workflow. Plaintext is returned once.
func (s *APIKeyService) Create(workflowID, name string) (CreateAPIKeyResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return CreateAPIKeyResult{}, errors.New("name is required")
	}
	var wf models.WorkflowDef
	if err := s.db.First(&wf, "id = ?", workflowID).Error; err != nil {
		return CreateAPIKeyResult{}, ErrWorkflowNotFound
	}
	plain, err := generateAPIKeyPlaintext()
	if err != nil {
		return CreateAPIKeyResult{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return CreateAPIKeyResult{}, fmt.Errorf("hash key: %w", err)
	}
	prefix := plain
	if len(plain) > 4 {
		prefix = "cf_wf_" + strings.Repeat("•", 16) + plain[len(plain)-4:]
	}
	key := models.WorkflowAPIKey{
		ID:         "key-" + uuid.NewString()[:8],
		WorkflowID: workflowID,
		Name:       name,
		KeyPrefix:  prefix,
		KeyHash:    string(hash),
		CreatedAt:  time.Now(),
	}
	if err := s.db.Create(&key).Error; err != nil {
		return CreateAPIKeyResult{}, err
	}
	return CreateAPIKeyResult{Key: key, Plaintext: plain}, nil
}

// List returns active (non-revoked) keys for a workflow, newest first.
func (s *APIKeyService) List(workflowID string) []models.WorkflowAPIKey {
	var keys []models.WorkflowAPIKey
	s.db.Where("workflow_id = ? AND revoked_at IS NULL", workflowID).
		Order("created_at desc").Find(&keys)
	return keys
}

// Revoke marks a key as revoked. Returns false when not found or already revoked.
func (s *APIKeyService) Revoke(workflowID, keyID string) bool {
	now := time.Now()
	res := s.db.Model(&models.WorkflowAPIKey{}).
		Where("id = ? AND workflow_id = ? AND revoked_at IS NULL", keyID, workflowID).
		Update("revoked_at", now)
	return res.RowsAffected > 0
}

// ValidateBearer checks a plaintext API key and returns the bound workflow ID.
// Invalid, revoked, or missing keys all return ok=false (caller returns 401).
func (s *APIKeyService) ValidateBearer(plain string) (workflowID string, ok bool) {
	plain = strings.TrimSpace(plain)
	if plain == "" || !strings.HasPrefix(plain, "cf_wf_") {
		return "", false
	}
	var keys []models.WorkflowAPIKey
	s.db.Where("revoked_at IS NULL").Find(&keys)
	for _, k := range keys {
		if bcrypt.CompareHashAndPassword([]byte(k.KeyHash), []byte(plain)) == nil {
			return k.WorkflowID, true
		}
	}
	return "", false
}

func generateAPIKeyPlaintext() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "cf_wf_" + hex.EncodeToString(b), nil
}
