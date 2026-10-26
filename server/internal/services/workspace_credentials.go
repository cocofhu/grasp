package services

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/crypto"
	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

var credentialEnvKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validCredentialEnvKey accepts an empty key (no binding) or a plain env
// identifier that does not collide with platform-injected sandbox variables.
func validCredentialEnvKey(k string) bool {
	if k == "" {
		return true
	}
	return credentialEnvKeyPattern.MatchString(k) && !envauth.IsPlatformReservedEnvKey(k)
}

// CredentialInput is the write shape used by the credential API.
// Value is optional on update: an omitted/blank value keeps the current secret
// unless Clear is true. The service never returns Value.
type CredentialInput struct {
	Purpose    string         `json:"purpose"`
	Issuer     string         `json:"issuer"`
	ToolID     string         `json:"toolId"`
	SecretKind string         `json:"secretKind"`
	Host       string         `json:"host"`
	Type       string         `json:"type"`
	Provider   string         `json:"provider"`
	Name       string         `json:"name"`
	Target     string         `json:"target"`
	TargetType string         `json:"targetType"`
	TargetID   string         `json:"targetId"`
	EnvKey     string         `json:"envKey"`
	Value      string         `json:"value"`
	Metadata   map[string]any `json:"metadata"`
	Enabled    *bool          `json:"enabled"`
	Clear      bool           `json:"clear"`
}

// CredentialView is safe for API/UI use. Masked is derived from the
// encrypted value and never contains any plaintext.
type CredentialView struct {
	Purpose        string         `json:"purpose,omitempty"`
	Issuer         string         `json:"issuer,omitempty"`
	ToolID         string         `json:"toolId,omitempty"`
	SecretKind     string         `json:"secretKind,omitempty"`
	Host           string         `json:"host,omitempty"`
	SecretRevision uint64         `json:"secretRevision"`
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	Provider       string         `json:"provider,omitempty"`
	Name           string         `json:"name"`
	Target         string         `json:"target,omitempty"`
	TargetType     string         `json:"targetType,omitempty"`
	TargetID       string         `json:"targetId,omitempty"`
	EnvKey         string         `json:"envKey,omitempty"`
	Masked         string         `json:"masked,omitempty"`
	Source         string         `json:"source,omitempty"`
	Configured     bool           `json:"configured"`
	Enabled        bool           `json:"enabled"`
	RevokedAt      *time.Time     `json:"revokedAt,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

var (
	ErrCredentialNotFound   = errors.New("credential not found")
	ErrCredentialType       = errors.New("credential type is required")
	ErrCredentialName       = errors.New("credential name is required")
	ErrCredentialTarget     = errors.New("custom credential target is required")
	ErrCredentialEnvKey     = errors.New("credential env key must be a valid identifier and not a platform-reserved key")
	ErrCredentialAliasTaken = errors.New("credential alias already exists for this kind")
	ErrCredentialPurpose    = errors.New("invalid credential purpose, tool or secret kind")
	// ErrCredentialNotRemovable rejects deleting a row that is not a workspace model API account.
	ErrCredentialNotRemovable = errors.New("only a workspace model API account can be removed")
)

// CredentialAliasTakenError is returned when a new alias matches another
// credential of the same kind. Alias is the existing display name.
type CredentialAliasTakenError struct{ Alias string }

func (e *CredentialAliasTakenError) Error() string { return ErrCredentialAliasTaken.Error() }
func (e *CredentialAliasTakenError) Unwrap() error { return ErrCredentialAliasTaken }

// CredentialService persists encrypted workspace credentials and resolves
// UI values for runtime injection.
type CredentialService struct{ db *gorm.DB }

func NewCredentialService(db *gorm.DB) *CredentialService {
	return &CredentialService{db: db}
}

// validCredentialType lists the types stored in Credential. Channel,
// external MCP and workflow keys are read-only adapter views over their own
// services and cannot be created here.
func validCredentialType(t string) bool {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "ai", "git", "ssh", "mcp", "custom":
		return true
	default:
		return false
	}
}

func defaultCredentialEnvKey(in CredentialInput) string {
	t := strings.ToLower(strings.TrimSpace(in.Type))
	p := strings.ToLower(strings.TrimSpace(in.Provider))
	switch t {
	case "ai":
		switch p {
		case "cursor":
			return envauth.EnvCursorAPIKey
		case "claude", "claude_code", "anthropic":
			return envauth.EnvClaudeAPIKey
		case "codebuddy":
			return envauth.EnvCodeBuddyAPIKey
		case "trae":
			return envauth.EnvTraeAPIKey
		case "codex":
			return envauth.EnvCodexAuthFile
		case "opencode":
			return envauth.EnvOpenCodeAPIKey
		case "deepseek_harness":
			return envauth.EnvDeepSeekAPIKey
		}
	case "git":
		switch p {
		case "github", "gh":
			return envauth.EnvGitHubToken
		case "gitlab", "glab":
			return envauth.EnvGitLabToken
		}
	case "ssh":
		if in.SecretKind == "ssh_known_hosts" {
			return envauth.EnvGitSSHKnownHosts
		}
		return envauth.EnvGitSSHPrivateKey
	}
	return ""
}

func credentialView(row models.Credential) CredentialView {
	purpose, issuer, tool, kind := credentialDomain(row)
	return CredentialView{
		Purpose: purpose, Issuer: issuer, ToolID: tool, SecretKind: kind, Host: row.Host, SecretRevision: row.SecretRevision,
		ID: row.ID, Type: row.Type, Provider: row.Provider,
		Name: row.Name, Target: row.Target, TargetID: row.Target, EnvKey: row.EnvKey,
		Masked: func() string {
			if strings.TrimSpace(row.ValueEnc) == "" {
				return ""
			}
			return "••••••••"
		}(), Source: "workspace", Configured: strings.TrimSpace(row.ValueEnc) != "",
		Enabled: row.Enabled, RevokedAt: row.RevokedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func (s *CredentialService) List() ([]CredentialView, error) {
	var rows []models.Credential
	if err := s.db.Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]CredentialView, 0, len(rows))
	for _, row := range rows {
		if !currentCredential(row) {
			continue
		}
		v := credentialView(row)
		out = append(out, v)
	}
	return out, nil
}

// Create stores a new encrypted credential. Plaintext is accepted only on
// write and is never returned by this service.
func (s *CredentialService) Create(in CredentialInput) (CredentialView, error) {
	if err := normalizeCredentialDomainInput(&in); err != nil {
		return CredentialView{}, err
	}
	in.Type = strings.ToLower(strings.TrimSpace(in.Type))
	in.Provider = strings.TrimSpace(in.Provider)
	in.Name = strings.TrimSpace(in.Name)
	in.EnvKey = strings.TrimSpace(in.EnvKey)
	if in.Type == "" || !validCredentialType(in.Type) {
		return CredentialView{}, ErrCredentialType
	}
	if in.Name == "" {
		return CredentialView{}, ErrCredentialName
	}
	if in.EnvKey == "" && in.Purpose != "model_api" {
		in.EnvKey = defaultCredentialEnvKey(in)
	}
	if !validCredentialEnvKey(in.EnvKey) {
		return CredentialView{}, ErrCredentialEnvKey
	}
	if in.Purpose == "custom" || in.Purpose == "mcp" {
		if envauth.IsSecretEnvKey(in.EnvKey) || runtime.IsGitAuthorizationEnvKey(in.EnvKey) {
			return CredentialView{}, ErrCredentialEnvKey
		}
	} else if in.Purpose != "model_api" && in.EnvKey != defaultCredentialEnvKey(in) {
		return CredentialView{}, ErrCredentialEnvKey
	}
	if in.Type == "custom" && strings.TrimSpace(in.EnvKey) == "" && strings.TrimSpace(in.Target) == "" && strings.TrimSpace(in.TargetID) == "" {
		return CredentialView{}, ErrCredentialTarget
	}
	if strings.TrimSpace(in.Value) == "" {
		return CredentialView{}, errors.New("credential value is required")
	}
	taken, ok, err := s.credentialAliasTaken(in.EnvKey, in.Name, "")
	if err != nil {
		return CredentialView{}, err
	}
	if ok {
		return CredentialView{}, &CredentialAliasTakenError{Alias: taken}
	}
	if err := rejectCodexLoginValue(in.Provider, in.EnvKey, in.Value); err != nil {
		return CredentialView{}, err
	}
	enc, err := crypto.Encrypt(in.Value)
	if err != nil {
		return CredentialView{}, fmt.Errorf("encrypt credential: %w", err)
	}
	target := strings.TrimSpace(in.Target)
	if target == "" {
		target = strings.TrimSpace(in.TargetID)
	}
	now := time.Now()
	row := models.Credential{ID: "cred-" + uuid.NewString()[:12],
		Purpose: in.Purpose, Issuer: in.Issuer, ToolID: in.ToolID, SecretKind: in.SecretKind, Host: in.Host, SecretRevision: 1,
		Type: in.Type, Provider: in.Provider, Name: in.Name, Target: target,
		EnvKey: in.EnvKey, ValueEnc: enc,
		Enabled: in.Enabled == nil || *in.Enabled, CreatedAt: now, UpdatedAt: now}
	if err := s.db.Create(&row).Error; err != nil {
		return CredentialView{}, err
	}
	return credentialView(row), nil
}

// Update edits metadata and optionally rotates/clears the secret.
func (s *CredentialService) Update(id string, in CredentialInput) (CredentialView, error) {
	var row models.Credential
	if err := s.db.Where("id = ?", strings.TrimSpace(id)).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CredentialView{}, ErrCredentialNotFound
		}
		return CredentialView{}, err
	}
	if !currentCredential(row) {
		return CredentialView{}, ErrCredentialNotFound
	}
	if in.Purpose != "" || row.Purpose != "" {
		purpose, issuer, tool, kind := credentialDomain(row)
		if in.Purpose != "" && in.Purpose != purpose {
			return CredentialView{}, ErrCredentialPurpose
		}
		if in.Purpose == "" {
			in.Purpose = purpose
		}
		if in.Issuer == "" {
			in.Issuer = issuer
		}
		if in.ToolID == "" {
			in.ToolID = tool
		}
		if in.SecretKind == "" {
			in.SecretKind = kind
		}
		// A metadata-only edit or key rotation keeps the provider identity. The
		// issuer may be a free-form display name, not a legacy tool provider.
		if in.Provider == "" {
			in.Provider = row.Provider
		}
		if in.Host == "" {
			in.Host = row.Host
		}
		if err := normalizeCredentialDomainInput(&in); err != nil {
			return CredentialView{}, err
		}
		row.Purpose, row.Issuer, row.ToolID, row.SecretKind, row.Host = in.Purpose, in.Issuer, in.ToolID, in.SecretKind, in.Host
	}
	if v := strings.TrimSpace(in.Type); v != "" {
		v = strings.ToLower(v)
		if !validCredentialType(v) {
			return CredentialView{}, ErrCredentialType
		}
		row.Type = v
	}
	if strings.TrimSpace(in.Provider) != "" {
		row.Provider = strings.TrimSpace(in.Provider)
	}
	nameChanged := strings.TrimSpace(in.Name) != ""
	if nameChanged {
		row.Name = strings.TrimSpace(in.Name)
	}
	if strings.TrimSpace(in.Target) != "" {
		row.Target = strings.TrimSpace(in.Target)
	}
	if strings.TrimSpace(in.TargetID) != "" {
		row.Target = strings.TrimSpace(in.TargetID)
	}
	if strings.TrimSpace(in.EnvKey) != "" {
		row.EnvKey = strings.TrimSpace(in.EnvKey)
	}
	if !validCredentialEnvKey(row.EnvKey) {
		return CredentialView{}, ErrCredentialEnvKey
	}
	if row.Purpose == "custom" || row.Purpose == "mcp" {
		if envauth.IsSecretEnvKey(row.EnvKey) || runtime.IsGitAuthorizationEnvKey(row.EnvKey) {
			return CredentialView{}, ErrCredentialEnvKey
		}
	} else if row.Purpose != "model_api" && row.EnvKey != defaultCredentialEnvKey(CredentialInput{Type: row.Type, Provider: row.Provider, SecretKind: row.SecretKind}) {
		return CredentialView{}, ErrCredentialEnvKey
	}
	if nameChanged {
		taken, ok, err := s.credentialAliasTaken(row.EnvKey, row.Name, row.ID)
		if err != nil {
			return CredentialView{}, err
		}
		if ok {
			return CredentialView{}, &CredentialAliasTakenError{Alias: taken}
		}
	}
	if in.Enabled != nil {
		row.Enabled = *in.Enabled
	}
	if row.Type == "custom" && strings.TrimSpace(row.EnvKey) == "" && strings.TrimSpace(row.Target) == "" {
		return CredentialView{}, ErrCredentialTarget
	}
	if row.RevokedAt != nil && in.Enabled == nil && strings.TrimSpace(in.Value) != "" {
		row.RevokedAt = nil
		row.Enabled = true
	}
	if !in.Clear && in.Value != "" {
		if err := rejectCodexLoginValue(row.Provider, row.EnvKey, in.Value); err != nil {
			return CredentialView{}, err
		}
	}
	if in.Clear {
		row.ValueEnc = ""
		row.SecretRevision = nextSecretRevision(row.SecretRevision)
	} else if strings.TrimSpace(in.Value) != "" {
		enc, err := crypto.Encrypt(in.Value)
		if err != nil {
			return CredentialView{}, fmt.Errorf("encrypt credential: %w", err)
		}
		row.ValueEnc = enc
		row.SecretRevision = nextSecretRevision(row.SecretRevision)
	}
	row.UpdatedAt = time.Now()
	if err := s.db.Save(&row).Error; err != nil {
		return CredentialView{}, err
	}
	return credentialView(row), nil
}

func (s *CredentialService) Revoke(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	now := time.Now()
	res := s.db.Model(&models.Credential{}).Where("id = ?", id).
		Updates(map[string]any{"revoked_at": now, "enabled": false, "updated_at": now, "secret_revision": gorm.Expr("secret_revision + 1")})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrCredentialNotFound
	}
	return nil
}

// Clear removes the secret from an explicitly created credential. Its current
// metadata remains editable; no replacement row or default slot is created.
func (s *CredentialService) Clear(id string) error {
	id = strings.TrimSpace(id)
	var row models.Credential
	if err := s.db.Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCredentialNotFound
		}
		return err
	}
	if !currentCredential(row) {
		return ErrCredentialNotFound
	}
	row.ValueEnc = ""
	row.SecretRevision = nextSecretRevision(row.SecretRevision)
	row.RevokedAt = nil
	row.Enabled = true
	row.UpdatedAt = time.Now()
	return s.db.Save(&row).Error
}

// RemoveModelAPI deletes a workspace-owned model_api account and the model
// connections bound to it. Clear only wipes the secret and keeps the row.
// Either the account and its connections all go, or a failure leaves both as they were.
func (s *CredentialService) RemoveModelAPI(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrCredentialNotFound
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var row models.Credential
		if err := tx.Where("id = ?", id).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCredentialNotFound
			}
			return err
		}
		if !currentCredential(row) {
			return ErrCredentialNotFound
		}
		purpose, _, _, _ := credentialDomain(row)
		if purpose != "model_api" {
			return ErrCredentialNotRemovable
		}
		if err := tx.Where("credential_id = ?", id).Delete(&models.ModelConnection{}).Error; err != nil {
			return err
		}
		res := tx.Delete(&models.Credential{}, "id = ?", id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrCredentialNotFound
		}
		return nil
	})
}

// Get returns one credential view. The secret is never included.
func (s *CredentialService) Get(id string) (CredentialView, error) {
	var row models.Credential
	if err := s.db.Where("id = ?", strings.TrimSpace(id)).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CredentialView{}, ErrCredentialNotFound
		}
		return CredentialView{}, err
	}
	if !currentCredential(row) {
		return CredentialView{}, ErrCredentialNotFound
	}
	return credentialView(row), nil
}

// ResolveEnv exposes only generic MCP and custom environment credentials.
// Model and Git authorizations are resolved exclusively by explicit bindings.
func (s *CredentialService) ResolveEnv() map[string]string {
	var rows []models.Credential
	if s.db.Where("enabled = ? AND revoked_at IS NULL", true).Find(&rows).Error != nil {
		return nil
	}
	out := map[string]string{}
	counts := map[string]int{}
	for _, row := range rows {
		if credentialEnvRow(row) && strings.TrimSpace(row.ValueEnc) != "" {
			counts[strings.TrimSpace(row.EnvKey)]++
		}
	}
	for _, row := range rows {
		key := strings.TrimSpace(row.EnvKey)
		if !credentialEnvRow(row) || counts[key] != 1 || strings.TrimSpace(row.ValueEnc) == "" {
			continue
		}
		value, err := crypto.Decrypt(row.ValueEnc)
		if err != nil {
			log.Warn().Err(err).Str("credential", row.ID).Msg("skip undecryptable workspace credential")
			continue
		}
		if value != "" {
			out[key] = value
		}
	}
	return out
}

// WriteBackCodexLoginCredential refreshes precisely the injected credential.
// Revocation, clearing and deletion while a sandbox runs are never undone.
func (s *CredentialService) WriteBackCodexLoginCredential(credentialID, content string) error {
	if err := runtime.ValidateCodexLoginFile(content); err != nil {
		return err
	}
	var row models.Credential
	if err := s.db.Where("id = ? AND env_key = ? AND enabled = ? AND revoked_at IS NULL AND value_enc <> ''", credentialID, envauth.EnvCodexAuthFile, true).First(&row).Error; err != nil {
		return err
	}
	if !currentCredential(row) || row.Purpose != "tool_auth" || row.ToolID != "codex" {
		return ErrCredentialNotFound
	}
	previous, err := crypto.Decrypt(row.ValueEnc)
	if err != nil {
		return err
	}
	if previous == content {
		return nil
	}
	enc, err := crypto.Encrypt(content)
	if err != nil {
		return err
	}
	result := s.db.Model(&models.Credential{}).Where("id = ? AND value_enc = ? AND enabled = ? AND revoked_at IS NULL", row.ID, row.ValueEnc, true).Updates(map[string]any{"value_enc": enc, "updated_at": time.Now(), "secret_revision": nextSecretRevision(row.SecretRevision)})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("Codex login credential changed during writeback")
	}
	return nil
}

func credentialAliasKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// credentialAliasTaken reports the existing display alias when another row of
// the same env key (the kind) already uses this name. Comparison trims and
// ignores case. exceptID is the row being renamed. A query error is returned
// to the caller; it is not treated as "alias available".
func (s *CredentialService) credentialAliasTaken(envKey, name, exceptID string) (string, bool, error) {
	envKey = strings.TrimSpace(envKey)
	want := credentialAliasKey(name)
	if envKey == "" || want == "" {
		return "", false, nil
	}
	var rows []models.Credential
	if err := s.db.Where("env_key = ?", envKey).Find(&rows).Error; err != nil {
		return "", false, err
	}
	for _, row := range rows {
		if row.ID == exceptID || !currentCredential(row) {
			continue
		}
		if credentialAliasKey(row.Name) == want {
			return strings.TrimSpace(row.Name), true, nil
		}
	}
	return "", false, nil
}

func credentialEnvRow(row models.Credential) bool {
	if !currentCredential(row) || row.Purpose != "mcp" && row.Purpose != "custom" {
		return false
	}
	if row.Type != "mcp" && row.Type != "custom" {
		return false
	}
	key := strings.TrimSpace(row.EnvKey)
	return key != "" && !envauth.IsSecretEnvKey(key) && !runtime.IsGitAuthorizationEnvKey(key) && !envauth.IsPlatformReservedEnvKey(key)
}

// ResolveSelectedValue returns one credential's env key and plaintext.
// An unknown, cleared, or empty secret returns ok=false and never substitutes
// another row of the same kind.
func (s *CredentialService) ResolveSelectedValue(credentialID string) (string, string, bool) {
	credentialID = strings.TrimSpace(credentialID)
	if s == nil || s.db == nil || credentialID == "" {
		return "", "", false
	}
	var row models.Credential
	err := s.db.Where("id = ? AND enabled = ? AND revoked_at IS NULL", credentialID, true).First(&row).Error
	if err != nil || !currentCredential(row) || strings.TrimSpace(row.ValueEnc) == "" {
		return "", "", false
	}
	dec, err := crypto.Decrypt(row.ValueEnc)
	if err != nil || strings.TrimSpace(dec) == "" {
		log.Warn().Err(err).Str("credential", row.ID).Msg("skip undecryptable workspace credential")
		return "", "", false
	}
	return strings.TrimSpace(row.EnvKey), dec, true
}

// ResolveGitValue reads the secret and host together. A host is never inferred
// from the Agent's scope or the repository that happened to request the token.
func (s *CredentialService) ResolveGitValue(id string) (runtime.GitCredentialValue, bool) {
	var value runtime.GitCredentialValue
	if s == nil || s.db == nil || strings.TrimSpace(id) == "" {
		return value, false
	}
	var row models.Credential
	if err := s.db.Where("id = ? AND enabled = ? AND revoked_at IS NULL", strings.TrimSpace(id), true).First(&row).Error; err != nil || !currentCredential(row) || row.ValueEnc == "" || row.Purpose != "git" && row.Purpose != "ssh" {
		return value, false
	}
	if row.Purpose == "git" {
		var err error
		value.Host, err = models.NormalizeGitCredentialHost(row.Provider, row.Host)
		if err != nil {
			return runtime.GitCredentialValue{}, false
		}
	}
	secret, err := crypto.Decrypt(row.ValueEnc)
	if err != nil || strings.TrimSpace(secret) == "" {
		return runtime.GitCredentialValue{}, false
	}
	value.EnvKey, value.Secret = row.EnvKey, secret
	return value, true
}

func isCodexCredential(provider, envKey string) bool {
	if strings.EqualFold(strings.TrimSpace(provider), "codex") {
		return true
	}
	return strings.TrimSpace(envKey) == envauth.EnvCodexAuthFile
}

func rejectCodexLoginValue(provider, envKey, value string) error {
	if !isCodexCredential(provider, envKey) {
		return nil
	}
	return runtime.ValidateCodexLoginFile(value)
}

// CredentialEnvKeys returns the environment keys bound by active workspace
// credentials that carry a value. Callers use this set
// to prevent a per-run environment snapshot from shadowing a workspace
// credential binding. Cleared credentials do not block Run env values.
// The values themselves are intentionally not resolved here.
func (s *CredentialService) CredentialEnvKeys() map[string]struct{} {
	var rows []models.Credential
	if s.db.Where("enabled = ? AND revoked_at IS NULL", true).Find(&rows).Error != nil {
		return nil
	}
	out := make(map[string]struct{})
	for _, row := range rows {
		if strings.TrimSpace(row.ValueEnc) == "" {
			continue
		}
		if credentialEnvRow(row) {
			out[strings.TrimSpace(row.EnvKey)] = struct{}{}
		}

	}
	return out
}

// ResolveReferences returns credential-id substitutions for MCP templates.
func (s *CredentialService) ResolveReferences() map[string]string {
	var rows []models.Credential
	if s.db.Where("enabled = ? AND revoked_at IS NULL", true).Find(&rows).Error != nil {
		return nil
	}
	out := map[string]string{}
	for _, row := range rows {
		if !currentCredential(row) {
			continue
		}
		switch row.Type {
		case "ai", "git", "ssh", "mcp", "custom":
		default:
			continue
		}
		v, err := crypto.Decrypt(row.ValueEnc)
		if err == nil && v != "" {
			out["credential:"+row.ID] = v
		}
	}
	return out
}
