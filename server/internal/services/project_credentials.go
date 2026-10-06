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

// overlayProjectCredentialEnv applies resolved project credentials onto env.
// Platform-reserved keys are skipped so a credential can never replace run
// coordinates or platform MCP tokens.
func overlayProjectCredentialEnv(env, creds map[string]string) {
	for k, v := range creds {
		k = strings.TrimSpace(k)
		if k == "" || envauth.IsPlatformReservedEnvKey(k) {
			continue
		}
		env[k] = v
	}
}

// ProjectCredentialInput is the write shape used by the project credential API.
// Value is optional on update: an omitted/blank value keeps the current secret
// unless Clear is true. The service never returns Value.
type ProjectCredentialInput struct {
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

// ProjectCredentialView is safe for API/UI use. Masked is derived from the
// encrypted value and never contains any plaintext.
type ProjectCredentialView struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"projectId"`
	Type       string         `json:"type"`
	Provider   string         `json:"provider,omitempty"`
	Name       string         `json:"name"`
	Target     string         `json:"target,omitempty"`
	TargetType string         `json:"targetType,omitempty"`
	TargetID   string         `json:"targetId,omitempty"`
	EnvKey     string         `json:"envKey,omitempty"`
	Masked     string         `json:"masked,omitempty"`
	Source     string         `json:"source,omitempty"`
	Configured bool           `json:"configured"`
	Enabled    bool           `json:"enabled"`
	RevokedAt  *time.Time     `json:"revokedAt,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
}

var (
	ErrCredentialNotFound = errors.New("project credential not found")
	ErrCredentialProject  = errors.New("project id required")
	ErrCredentialType     = errors.New("credential type is required")
	ErrCredentialName     = errors.New("credential name is required")
	ErrCredentialTarget   = errors.New("custom credential target is required")
	ErrCredentialEnvKey   = errors.New("credential env key must be a valid identifier and not a platform-reserved key")
)

// ProjectCredentialService persists encrypted project credentials and resolves
// UI values for runtime injection.
type ProjectCredentialService struct{ db *gorm.DB }

func NewProjectCredentialService(db *gorm.DB) *ProjectCredentialService {
	return &ProjectCredentialService{db: db}
}

func (s *ProjectCredentialService) ensureProject(projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return ErrCredentialProject
	}
	var p models.Project
	err := s.db.Select("id").First(&p, "id = ?", projectID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrProjectNotFound
	}
	return err
}

// validCredentialType lists the types stored in ProjectCredential. Channel,
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

func defaultCredentialEnvKey(in ProjectCredentialInput) string {
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
		case "opencode":
			return envauth.EnvOpenCodeAPIKey
		case "codex":
			return envauth.EnvCodexAuthFile
		}
	case "git":
		switch p {
		case "github", "gh":
			return envauth.EnvGitHubToken
		case "gitlab", "glab":
			return envauth.EnvGitLabToken
		}
	case "ssh":
		return envauth.EnvGitSSHPrivateKey
	}
	return ""
}

func credentialView(row models.ProjectCredential) ProjectCredentialView {
	return ProjectCredentialView{
		ID: row.ID, ProjectID: row.ProjectID, Type: row.Type, Provider: row.Provider,
		Name: row.Name, Target: row.Target, TargetID: row.Target, EnvKey: row.EnvKey,
		Masked: func() string {
			if strings.TrimSpace(row.ValueEnc) == "" {
				return ""
			}
			return "••••••••"
		}(), Source: "project", Configured: strings.TrimSpace(row.ValueEnc) != "",
		Enabled: row.Enabled, RevokedAt: row.RevokedAt, Metadata: safeCredentialMetadata(row.Metadata),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

// safeCredentialMetadata keeps the API response limited to documented,
// non-sensitive OpenCode routing fields. Secret material belongs in ValueEnc;
// arbitrary metadata must not become a second plaintext escape hatch.
func safeCredentialMetadata(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{"provider", "baseUrl", "model"} {
		if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	// Vision is a routing capability for OpenCode's manually declared models,
	// rather than secret material. Preserve the boolean so the runtime can opt
	// image input in for gateways that are absent from models.dev.
	if value, ok := metadata["vision"].(bool); ok {
		out["vision"] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ensureDefaultRows creates empty, stable slots for the built-in credentials
// shown by the project UI. Empty rows do not affect runtime resolution and are
// not a migration of legacy environment values.
func (s *ProjectCredentialService) ensureDefaultRows(projectID string) error {
	defaults := []models.ProjectCredential{
		{ID: "cred-" + projectID + "-cursor", Type: "ai", Provider: "cursor", Name: "Cursor API Key", EnvKey: envauth.EnvCursorAPIKey},
		{ID: "cred-" + projectID + "-claude", Type: "ai", Provider: "claude_code", Name: "Claude Code API Key", EnvKey: envauth.EnvClaudeAPIKey},
		{ID: "cred-" + projectID + "-codebuddy", Type: "ai", Provider: "codebuddy", Name: "CodeBuddy API Key", EnvKey: envauth.EnvCodeBuddyAPIKey},
		{ID: "cred-" + projectID + "-trae", Type: "ai", Provider: "trae", Name: "Trae API Token", EnvKey: envauth.EnvTraeAPIKey},
		{ID: "cred-" + projectID + "-opencode", Type: "ai", Provider: "opencode", Name: "OpenCode API Key", EnvKey: envauth.EnvOpenCodeAPIKey},
		{ID: "cred-" + projectID + "-codex", Type: "ai", Provider: "codex", Name: "Codex Login File", EnvKey: envauth.EnvCodexAuthFile},
		{ID: "cred-" + projectID + "-github", Type: "git", Provider: "github", Name: "GitHub HTTPS Token", EnvKey: envauth.EnvGitHubToken},
		{ID: "cred-" + projectID + "-gitlab", Type: "git", Provider: "gitlab", Name: "GitLab HTTPS Token", EnvKey: envauth.EnvGitLabToken},
		{ID: "cred-" + projectID + "-gitlab-url", Type: "git", Provider: "gitlab", Name: "GitLab URL", EnvKey: "GITLAB_URL"},
		{ID: "cred-" + projectID + "-ssh-key", Type: "ssh", Provider: "ssh", Name: "Git SSH Private Key", EnvKey: envauth.EnvGitSSHPrivateKey},
		{ID: "cred-" + projectID + "-ssh-hosts", Type: "ssh", Provider: "ssh", Name: "Git SSH Known Hosts", EnvKey: envauth.EnvGitSSHKnownHosts},
	}
	for _, row := range defaults {
		var existing models.ProjectCredential
		err := s.db.Where("id = ?", row.ID).First(&existing).Error
		if err == nil {
			continue
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		now := time.Now()
		row.ProjectID, row.Enabled, row.CreatedAt, row.UpdatedAt = projectID, true, now, now
		if err := s.db.Create(&row).Error; err != nil && !strings.Contains(strings.ToLower(err.Error()), "unique") {
			return err
		}
	}
	return nil
}

func (s *ProjectCredentialService) List(projectID string) ([]ProjectCredentialView, error) {
	if err := s.ensureProject(projectID); err != nil {
		return nil, err
	}
	if err := s.ensureDefaultRows(projectID); err != nil {
		return nil, err
	}
	var rows []models.ProjectCredential
	if err := s.db.Where("project_id = ?", projectID).Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ProjectCredentialView, 0, len(rows))
	for _, row := range rows {
		v := credentialView(row)
		out = append(out, v)
	}
	return out, nil
}

// Create stores a new encrypted credential. Plaintext is accepted only on
// write and is never returned by this service.
func (s *ProjectCredentialService) Create(projectID string, in ProjectCredentialInput) (ProjectCredentialView, error) {
	if err := s.ensureProject(projectID); err != nil {
		return ProjectCredentialView{}, err
	}
	in.Type = strings.ToLower(strings.TrimSpace(in.Type))
	in.Provider = strings.TrimSpace(in.Provider)
	in.Name = strings.TrimSpace(in.Name)
	in.EnvKey = strings.TrimSpace(in.EnvKey)
	if in.Type == "" || !validCredentialType(in.Type) {
		return ProjectCredentialView{}, ErrCredentialType
	}
	if in.Name == "" {
		return ProjectCredentialView{}, ErrCredentialName
	}
	if in.EnvKey == "" {
		in.EnvKey = defaultCredentialEnvKey(in)
	}
	if !validCredentialEnvKey(in.EnvKey) {
		return ProjectCredentialView{}, ErrCredentialEnvKey
	}
	if in.Type == "custom" && strings.TrimSpace(in.EnvKey) == "" && strings.TrimSpace(in.Target) == "" && strings.TrimSpace(in.TargetID) == "" {
		return ProjectCredentialView{}, ErrCredentialTarget
	}
	if strings.TrimSpace(in.Value) == "" {
		return ProjectCredentialView{}, errors.New("credential value is required")
	}
	if err := rejectCodexLoginValue(in.Provider, in.EnvKey, in.Value); err != nil {
		return ProjectCredentialView{}, err
	}
	enc, err := crypto.Encrypt(in.Value)
	if err != nil {
		return ProjectCredentialView{}, fmt.Errorf("encrypt credential: %w", err)
	}
	target := strings.TrimSpace(in.Target)
	if target == "" {
		target = strings.TrimSpace(in.TargetID)
	}
	now := time.Now()
	row := models.ProjectCredential{ID: "cred-" + uuid.NewString()[:12], ProjectID: projectID,
		Type: in.Type, Provider: in.Provider, Name: in.Name, Target: target,
		EnvKey: in.EnvKey, ValueEnc: enc, Metadata: safeCredentialMetadata(in.Metadata),
		Enabled: in.Enabled == nil || *in.Enabled, CreatedAt: now, UpdatedAt: now}
	if err := s.db.Create(&row).Error; err != nil {
		return ProjectCredentialView{}, err
	}
	return credentialView(row), nil
}

// Update edits metadata and optionally rotates/clears the secret.
func (s *ProjectCredentialService) Update(projectID, id string, in ProjectCredentialInput) (ProjectCredentialView, error) {
	if err := s.ensureProject(projectID); err != nil {
		return ProjectCredentialView{}, err
	}
	var row models.ProjectCredential
	if err := s.db.Where("id = ? AND project_id = ?", strings.TrimSpace(id), projectID).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ProjectCredentialView{}, ErrCredentialNotFound
		}
		return ProjectCredentialView{}, err
	}
	if v := strings.TrimSpace(in.Type); v != "" {
		v = strings.ToLower(v)
		if !validCredentialType(v) {
			return ProjectCredentialView{}, ErrCredentialType
		}
		row.Type = v
	}
	if strings.TrimSpace(in.Provider) != "" {
		row.Provider = strings.TrimSpace(in.Provider)
	}
	if strings.TrimSpace(in.Name) != "" {
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
		return ProjectCredentialView{}, ErrCredentialEnvKey
	}
	if in.Metadata != nil {
		row.Metadata = safeCredentialMetadata(in.Metadata)
	}
	if in.Enabled != nil {
		row.Enabled = *in.Enabled
	}
	if row.Type == "custom" && strings.TrimSpace(row.EnvKey) == "" && strings.TrimSpace(row.Target) == "" {
		return ProjectCredentialView{}, ErrCredentialTarget
	}
	if row.RevokedAt != nil && in.Enabled == nil && strings.TrimSpace(in.Value) != "" {
		row.RevokedAt = nil
		row.Enabled = true
	}
	if !in.Clear && in.Value != "" {
		if err := rejectCodexLoginValue(row.Provider, row.EnvKey, in.Value); err != nil {
			return ProjectCredentialView{}, err
		}
	}
	if in.Clear {
		row.ValueEnc = ""
	} else if strings.TrimSpace(in.Value) != "" {
		enc, err := crypto.Encrypt(in.Value)
		if err != nil {
			return ProjectCredentialView{}, fmt.Errorf("encrypt credential: %w", err)
		}
		row.ValueEnc = enc
	}
	row.UpdatedAt = time.Now()
	if err := s.db.Save(&row).Error; err != nil {
		return ProjectCredentialView{}, err
	}
	return credentialView(row), nil
}

// SetByEnvKey creates or rotates the credential slot identified by envKey.
// It is used by onboarding so newly submitted secrets land in the project
// credential store instead of being written to shared Agent env.
func (s *ProjectCredentialService) SetByEnvKey(projectID string, in ProjectCredentialInput) (ProjectCredentialView, error) {
	key := strings.TrimSpace(in.EnvKey)
	if key == "" {
		return ProjectCredentialView{}, errors.New("credential env key is required")
	}
	// Materialize built-in slots before lookup so onboarding and API writes
	// update the stable UI row instead of creating a duplicate binding.
	if err := s.ensureProject(projectID); err != nil {
		return ProjectCredentialView{}, err
	}
	if err := s.ensureDefaultRows(projectID); err != nil {
		return ProjectCredentialView{}, err
	}
	var row models.ProjectCredential
	err := s.db.Where("project_id = ? AND env_key = ?", projectID, key).Order("created_at asc").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.Create(projectID, in)
	}
	if err != nil {
		return ProjectCredentialView{}, err
	}
	return s.Update(projectID, row.ID, in)
}

func (s *ProjectCredentialService) Revoke(projectID, id string) error {
	if err := s.ensureProject(projectID); err != nil {
		return err
	}
	now := time.Now()
	res := s.db.Model(&models.ProjectCredential{}).Where("id = ? AND project_id = ?", id, projectID).
		Updates(map[string]any{"revoked_at": now, "enabled": false, "updated_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrCredentialNotFound
	}
	return nil
}

// Clear removes the encrypted value while retaining the credential slot for
// later replacement. This is the DELETE semantics exposed by the UI.
func (s *ProjectCredentialService) Clear(projectID, id string) error {
	if err := s.ensureProject(projectID); err != nil {
		return err
	}
	res := s.db.Model(&models.ProjectCredential{}).
		Where("id = ? AND project_id = ?", id, projectID).
		Updates(map[string]any{"value_enc": "", "revoked_at": nil, "enabled": true, "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrCredentialNotFound
	}
	return nil
}

// ResolveEnv decrypts active runtime credentials. Service-only credentials are
// deliberately excluded so channel/API authentication can never leak into a
// sandbox environment.
func (s *ProjectCredentialService) ResolveEnv(projectID string) map[string]string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	var rows []models.ProjectCredential
	if s.db.Where("project_id = ? AND enabled = ? AND revoked_at IS NULL", projectID, true).Order("updated_at desc").Find(&rows).Error != nil {
		return nil
	}
	out := map[string]string{}
	for _, row := range rows {
		switch strings.ToLower(strings.TrimSpace(row.Type)) {
		case "ai", "git", "ssh", "mcp", "custom":
		default:
			continue
		}
		key := strings.TrimSpace(row.EnvKey)
		if key == "" || envauth.IsPlatformReservedEnvKey(key) {
			continue
		}
		v := ""
		if strings.TrimSpace(row.ValueEnc) != "" {
			dec, err := crypto.Decrypt(row.ValueEnc)
			if err != nil {
				log.Warn().Err(err).Str("project", projectID).Str("credential", row.ID).Msg("skip undecryptable project credential")
			}
			v = dec
		}
		if v == "" {
			if strings.EqualFold(row.Provider, "opencode") {
				addOpenCodeMetadata(out, row.Metadata)
			}
			continue
		}
		if _, exists := out[key]; exists {
			continue
		}
		out[key] = v
		if strings.EqualFold(row.Provider, "opencode") {
			addOpenCodeMetadata(out, row.Metadata)
		}
	}
	return out
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

func aiCredentialName(backend string) string {
	if NormalizeAcpBackend(backend) == AcpBackendCodex {
		return "Codex Login File"
	}
	return backend + " API Key"
}

// WriteBackCodexLoginFile replaces the project's Codex login file when a run
// produced a different auth.json. Identical content does not touch UpdatedAt.
// The plaintext is never logged or returned.
func (s *ProjectCredentialService) WriteBackCodexLoginFile(projectID, content string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ErrCredentialProject
	}
	if err := runtime.ValidateCodexLoginFile(content); err != nil {
		return err
	}
	if err := s.ensureProject(projectID); err != nil {
		return err
	}
	if err := s.ensureDefaultRows(projectID); err != nil {
		return err
	}
	var row models.ProjectCredential
	err := s.db.Where("project_id = ? AND env_key = ?", projectID, envauth.EnvCodexAuthFile).
		Order("created_at asc").First(&row).Error
	if err != nil {
		return err
	}
	if strings.TrimSpace(row.ValueEnc) != "" {
		cur, decErr := crypto.Decrypt(row.ValueEnc)
		if decErr == nil && cur == content {
			return nil
		}
	}
	enc, err := crypto.Encrypt(content)
	if err != nil {
		return fmt.Errorf("encrypt credential: %w", err)
	}
	now := time.Now()
	return s.db.Model(&models.ProjectCredential{}).Where("id = ? AND project_id = ?", row.ID, projectID).
		Updates(map[string]any{"value_enc": enc, "updated_at": now, "enabled": true, "revoked_at": nil}).Error
}

func addOpenCodeMetadata(out map[string]string, metadata map[string]any) {
	for metaKey, envKey := range map[string]string{
		"provider": runtime.EnvOpenCodeProvider,
		"baseUrl":  runtime.EnvOpenCodeBaseURL,
		// The ACP bridge receives its selected model through ACP_BRIDGE_MODEL;
		// GRASP_OPENCODE_MODEL was never consumed by the runtime.
		"model": runtime.EnvACPBridgeModel,
	} {
		if raw, ok := metadata[metaKey].(string); ok && strings.TrimSpace(raw) != "" {
			out[envKey] = raw
		}
	}
	if vision, ok := metadata["vision"].(bool); ok {
		if vision {
			out[runtime.EnvOpenCodeModelVision] = "1"
		} else {
			delete(out, runtime.EnvOpenCodeModelVision)
		}
	}
}

// CredentialEnvKeys returns the environment keys bound by active project
// credentials that carry a value. Callers use this set
// to prevent a per-run environment snapshot from shadowing a project
// credential binding. Empty built-in slots (materialized just by opening the
// UI) are excluded so they do not silently block Run env values.
// The values themselves are intentionally not resolved here.
func (s *ProjectCredentialService) CredentialEnvKeys(projectID string) map[string]struct{} {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	var rows []models.ProjectCredential
	if s.db.Where("project_id = ? AND enabled = ? AND revoked_at IS NULL", projectID, true).Find(&rows).Error != nil {
		return nil
	}
	out := make(map[string]struct{})
	for _, row := range rows {
		if strings.TrimSpace(row.ValueEnc) == "" {
			continue
		}
		if key := strings.TrimSpace(row.EnvKey); key != "" {
			switch strings.ToLower(strings.TrimSpace(row.Type)) {
			case "ai", "git", "ssh", "mcp", "custom":
				out[key] = struct{}{}
			}
		}
	}
	return out
}

// ResolveReferences returns credential-id substitutions for MCP templates.
func (s *ProjectCredentialService) ResolveReferences(projectID string) map[string]string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	var rows []models.ProjectCredential
	if s.db.Where("project_id = ? AND enabled = ? AND revoked_at IS NULL", projectID, true).Find(&rows).Error != nil {
		return nil
	}
	out := map[string]string{}
	for _, row := range rows {
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
