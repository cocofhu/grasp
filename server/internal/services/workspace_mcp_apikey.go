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

const mcpKeyPrefix = "cf_ws_"

// WorkspaceMcpApiKeyService manages external MCP API keys.
type WorkspaceMcpApiKeyService struct{ db *gorm.DB }

// NewWorkspaceMcpApiKeyService builds the service.
func NewWorkspaceMcpApiKeyService(db *gorm.DB) *WorkspaceMcpApiKeyService {
	return &WorkspaceMcpApiKeyService{db: db}
}

// CreateMcpKeyResult holds the one-time plaintext key.
type CreateMcpKeyResult struct {
	Key       models.WorkspaceMcpApiKey
	Plaintext string
}

// Create generates a named key. Plaintext is returned once.
func (s *WorkspaceMcpApiKeyService) Create(name string) (CreateMcpKeyResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return CreateMcpKeyResult{}, errors.New("name is required")
	}
	plain, err := generateMcpKeyPlaintext()
	if err != nil {
		return CreateMcpKeyResult{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return CreateMcpKeyResult{}, fmt.Errorf("hash key: %w", err)
	}
	prefix := plain
	if len(plain) > 4 {
		prefix = mcpKeyPrefix + strings.Repeat("•", 16) + plain[len(plain)-4:]
	}
	key := models.WorkspaceMcpApiKey{
		ID:        "pmkey-" + uuid.NewString()[:8],
		Name:      name,
		KeyPrefix: prefix,
		KeyHash:   string(hash),
		CreatedAt: time.Now(),
	}
	if err := s.db.Create(&key).Error; err != nil {
		return CreateMcpKeyResult{}, err
	}
	return CreateMcpKeyResult{Key: key, Plaintext: plain}, nil
}

// List returns active keys, newest first.
func (s *WorkspaceMcpApiKeyService) List() []models.WorkspaceMcpApiKey {
	var keys []models.WorkspaceMcpApiKey
	s.db.Where("revoked_at IS NULL").
		Order("created_at desc").Find(&keys)
	return keys
}

// Revoke marks a key revoked. Returns false when not found or already revoked.
func (s *WorkspaceMcpApiKeyService) Revoke(keyID string) bool {
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return false
	}
	now := time.Now()
	res := s.db.Model(&models.WorkspaceMcpApiKey{}).
		Where("id = ? AND revoked_at IS NULL", keyID).
		Update("revoked_at", now)
	return res.RowsAffected > 0
}

// ValidateBearer checks plaintext and returns key metadata when valid.
func (s *WorkspaceMcpApiKeyService) ValidateBearer(plain string) (keyID, keyName string, ok bool) {
	plain = strings.TrimSpace(plain)
	if plain == "" || !strings.HasPrefix(plain, mcpKeyPrefix) {
		return "", "", false
	}
	var keys []models.WorkspaceMcpApiKey
	s.db.Where("revoked_at IS NULL").Find(&keys)
	for _, k := range keys {
		if bcrypt.CompareHashAndPassword([]byte(k.KeyHash), []byte(plain)) == nil {
			return k.ID, k.Name, true
		}
	}
	return "", "", false
}

func generateMcpKeyPlaintext() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return mcpKeyPrefix + hex.EncodeToString(b), nil
}
