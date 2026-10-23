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

// overlayCredentialEnv applies resolved workspace credentials onto env.
// Platform-reserved keys are skipped so a credential can never replace run
// coordinates or platform MCP tokens.
func overlayCredentialEnv(env, creds map[string]string) {
	for k, v := range creds {
		k = strings.TrimSpace(k)
		if k == "" || envauth.IsPlatformReservedEnvKey(k) {
			continue
		}
		env[k] = v
	}
}

// CredentialInput is the write shape used by the credential API.
// Value is optional on update: an omitted/blank value keeps the current secret
// unless Clear is true. The service never returns Value.
type CredentialInput struct {
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
	ID         string         `json:"id"`
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
	ErrCredentialNotFound   = errors.New("credential not found")
	ErrCredentialType       = errors.New("credential type is required")
	ErrCredentialName       = errors.New("credential name is required")
	ErrCredentialTarget     = errors.New("custom credential target is required")
	ErrCredentialEnvKey     = errors.New("credential env key must be a valid identifier and not a platform-reserved key")
	ErrCredentialModel      = errors.New("model is required")
	ErrCredentialBaseURL    = errors.New("base url is required for a custom provider")
	ErrCredentialAliasTaken = errors.New("credential alias already exists for this kind")
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
		case "opencode":
			return envauth.EnvOpenCodeAPIKey
		case "codex":
			return envauth.EnvCodexAuthFile
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
		return envauth.EnvGitSSHPrivateKey
	}
	return ""
}

func credentialView(row models.Credential) CredentialView {
	return CredentialView{
		ID: row.ID, Type: row.Type, Provider: row.Provider,
		Name: row.Name, Target: row.Target, TargetID: row.Target, EnvKey: row.EnvKey,
		Masked: func() string {
			if strings.TrimSpace(row.ValueEnc) == "" {
				return ""
			}
			return "••••••••"
		}(), Source: "workspace", Configured: strings.TrimSpace(row.ValueEnc) != "",
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

// Short product names for the built-in credential slots. GitLab URL stays
// qualified so it does not collide with the GitLab token slot.
const (
	builtinNameCursor          = "Cursor"
	builtinNameClaudeCode      = "Claude Code"
	builtinNameCodeBuddy       = "CodeBuddy"
	builtinNameTrae            = "Trae"
	builtinNameOpenCode        = "OpenCode"
	builtinNameCodex           = "Codex"
	builtinNameDeepSeekHarness = "DeepSeek Harness"
	builtinNameGitHub          = "GitHub"
	builtinNameGitLab          = "GitLab"
	builtinNameGitLabURL       = "GitLab URL"
	builtinNameSSHKey          = "SSH Key"
	builtinNameSSHHosts        = "SSH Hosts"
)

func builtinCredentialID(slot string) string { return "cred-builtin-" + slot }

func builtinCredentialRows() []models.Credential {
	return []models.Credential{
		{ID: builtinCredentialID("cursor"), Type: "ai", Provider: "cursor", Name: builtinNameCursor, EnvKey: envauth.EnvCursorAPIKey},
		{ID: builtinCredentialID("claude"), Type: "ai", Provider: "claude_code", Name: builtinNameClaudeCode, EnvKey: envauth.EnvClaudeAPIKey},
		{ID: builtinCredentialID("codebuddy"), Type: "ai", Provider: "codebuddy", Name: builtinNameCodeBuddy, EnvKey: envauth.EnvCodeBuddyAPIKey},
		{ID: builtinCredentialID("trae"), Type: "ai", Provider: "trae", Name: builtinNameTrae, EnvKey: envauth.EnvTraeAPIKey},
		{ID: builtinCredentialID("opencode"), Type: "ai", Provider: "opencode", Name: builtinNameOpenCode, EnvKey: envauth.EnvOpenCodeAPIKey},
		{ID: builtinCredentialID("deepseek_harness"), Type: "ai", Provider: "deepseek_harness", Name: builtinNameDeepSeekHarness, EnvKey: envauth.EnvDeepSeekAPIKey},
		{ID: builtinCredentialID("codex"), Type: "ai", Provider: "codex", Name: builtinNameCodex, EnvKey: envauth.EnvCodexAuthFile},
		{ID: builtinCredentialID("github"), Type: "git", Provider: "github", Name: builtinNameGitHub, EnvKey: envauth.EnvGitHubToken},
		{ID: builtinCredentialID("gitlab"), Type: "git", Provider: "gitlab", Name: builtinNameGitLab, EnvKey: envauth.EnvGitLabToken},
		{ID: builtinCredentialID("gitlab-url"), Type: "git", Provider: "gitlab", Name: builtinNameGitLabURL, EnvKey: "GITLAB_URL"},
		{ID: builtinCredentialID("ssh-key"), Type: "ssh", Provider: "ssh", Name: builtinNameSSHKey, EnvKey: envauth.EnvGitSSHPrivateKey},
		{ID: builtinCredentialID("ssh-hosts"), Type: "ssh", Provider: "ssh", Name: builtinNameSSHHosts, EnvKey: envauth.EnvGitSSHKnownHosts},
	}
}

// ensureDefaultRows creates empty, stable slots for the built-in credentials
// shown by the workspace UI. Empty rows do not affect runtime resolution.
func (s *CredentialService) ensureDefaultRows() error {
	for _, row := range builtinCredentialRows() {
		var n int64
		if err := s.db.Model(&models.Credential{}).Where("id = ?", row.ID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		now := time.Now()
		row.Enabled, row.CreatedAt, row.UpdatedAt = true, now, now
		row.BuiltinSlot = row.EnvKey
		if err := s.db.Create(&row).Error; err != nil && !strings.Contains(strings.ToLower(err.Error()), "unique") {
			return err
		}
	}
	return nil
}

func (s *CredentialService) List() ([]CredentialView, error) {
	if err := s.ensureDefaultRows(); err != nil {
		return nil, err
	}
	var rows []models.Credential
	if err := s.db.Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]CredentialView, 0, len(rows))
	for _, row := range rows {
		v := credentialView(row)
		out = append(out, v)
	}
	return out, nil
}

// Create stores a new encrypted credential. Plaintext is accepted only on
// write and is never returned by this service.
func (s *CredentialService) Create(in CredentialInput) (CredentialView, error) {
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
	if in.EnvKey == "" {
		in.EnvKey = defaultCredentialEnvKey(in)
	}
	if !validCredentialEnvKey(in.EnvKey) {
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
	if err := validateOpenCodeMetadata(in.Provider, in.Metadata, true); err != nil {
		return CredentialView{}, err
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
		Type: in.Type, Provider: in.Provider, Name: in.Name, Target: target,
		EnvKey: in.EnvKey, ValueEnc: enc, Metadata: safeCredentialMetadata(in.Metadata),
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
	if nameChanged {
		taken, ok, err := s.credentialAliasTaken(row.EnvKey, row.Name, row.ID)
		if err != nil {
			return CredentialView{}, err
		}
		if ok {
			return CredentialView{}, &CredentialAliasTakenError{Alias: taken}
		}
	}
	if in.Metadata != nil {
		provider := row.Provider
		if strings.TrimSpace(in.Provider) != "" {
			provider = strings.TrimSpace(in.Provider)
		}
		if err := validateOpenCodeMetadata(provider, in.Metadata, true); err != nil {
			return CredentialView{}, err
		}
		row.Metadata = safeCredentialMetadata(in.Metadata)
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
	} else if strings.TrimSpace(in.Value) != "" {
		enc, err := crypto.Encrypt(in.Value)
		if err != nil {
			return CredentialView{}, fmt.Errorf("encrypt credential: %w", err)
		}
		row.ValueEnc = enc
	}
	row.UpdatedAt = time.Now()
	if err := s.db.Save(&row).Error; err != nil {
		return CredentialView{}, err
	}
	return credentialView(row), nil
}

// SetByEnvKey creates or rotates the credential slot identified by envKey.
// It is used by onboarding so newly submitted secrets land in the workspace
// credential store instead of being written to shared Agent env.
func (s *CredentialService) SetByEnvKey(in CredentialInput) (CredentialView, error) {
	key := strings.TrimSpace(in.EnvKey)
	if key == "" {
		return CredentialView{}, errors.New("credential env key is required")
	}
	// Materialize built-in slots before lookup so onboarding and API writes
	// update the stable UI row instead of creating a duplicate binding.
	if err := s.ensureDefaultRows(); err != nil {
		return CredentialView{}, err
	}
	var row models.Credential
	err := s.db.Where("env_key = ?", key).Order("created_at asc").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.Create(in)
	}
	if err != nil {
		return CredentialView{}, err
	}
	return s.Update(row.ID, in)
}

func (s *CredentialService) Revoke(id string) error {
	now := time.Now()
	res := s.db.Model(&models.Credential{}).Where("id = ?", id).
		Updates(map[string]any{"revoked_at": now, "enabled": false, "updated_at": now})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrCredentialNotFound
	}
	return nil
}

// Clear removes a credential from use. Built-in slots keep their row so the
// workspace page still has a place to configure them. An extra model-vendor key
// is deleted so the row disappears. Callers that track an Agent selection
// should forget that id after a successful clear.
func (s *CredentialService) Clear(id string) error {
	id = strings.TrimSpace(id)
	var row models.Credential
	if err := s.db.Where("id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCredentialNotFound
		}
		return err
	}
	if isOpenCodeModelCredentialRow(row) && row.ID != builtinCredentialID("opencode") {
		return s.db.Delete(&models.Credential{}, "id = ?", row.ID).Error
	}
	row.ValueEnc = ""
	row.RevokedAt = nil
	row.Enabled = true
	row.UpdatedAt = time.Now()
	if isOpenCodeModelCredentialRow(row) {
		row.Metadata = map[string]any{}
	}
	return s.db.Save(&row).Error
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
	return credentialView(row), nil
}

// ResolveEnv decrypts active runtime credentials. Service-only credentials are
// deliberately excluded so channel/API authentication can never leak into a
// sandbox environment.
func (s *CredentialService) ResolveEnv() map[string]string {
	var rows []models.Credential
	if s.db.Where("enabled = ? AND revoked_at IS NULL", true).Order("updated_at desc").Find(&rows).Error != nil {
		return nil
	}
	out := map[string]string{}
	// A kind injects automatically only while a single row still holds a
	// secret. Empty built-in slots do not count: they exist before anyone adds
	// a second credential. An extra row whose secret was cleared does count,
	// so the remaining built-in secret is not restored until the Agent picks
	// one again. Two rows that both still have secrets are also withheld.
	type envKind struct {
		valuedID     string
		valued       int
		clearedExtra bool
	}
	kinds := map[string]*envKind{}
	for _, row := range rows {
		if !credentialEnvRow(row) {
			continue
		}
		key := strings.TrimSpace(row.EnvKey)
		kind := kinds[key]
		if kind == nil {
			kind = &envKind{}
			kinds[key] = kind
		}
		if strings.TrimSpace(row.ValueEnc) == "" {
			if row.BuiltinSlot == "" {
				kind.clearedExtra = true
			}
			continue
		}
		kind.valued++
		kind.valuedID = row.ID
	}
	suppressed := map[string]struct{}{}
	for _, row := range rows {
		switch strings.ToLower(strings.TrimSpace(row.Type)) {
		case "ai", "git", "ssh", "mcp", "custom":
		default:
			continue
		}
		// Model-vendor keys share one env name. Merging them here would inject
		// a key the Agent did not choose. ResolveOpenCodeCredential applies
		// only the selected row.
		if isOpenCodeModelCredentialRow(row) {
			continue
		}
		key := strings.TrimSpace(row.EnvKey)
		if key == "" || envauth.IsPlatformReservedEnvKey(key) {
			continue
		}
		kind := kinds[key]
		if kind == nil || kind.valued != 1 || kind.clearedExtra || kind.valuedID != row.ID {
			continue
		}
		if _, skip := suppressed[key]; skip {
			continue
		}
		v := ""
		if strings.TrimSpace(row.ValueEnc) != "" {
			dec, err := crypto.Decrypt(row.ValueEnc)
			if err != nil {
				log.Warn().Err(err).Str("credential", row.ID).Msg("skip undecryptable workspace credential")
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
			delete(out, key)
			suppressed[key] = struct{}{}
			continue
		}
		out[key] = v
		if strings.EqualFold(row.Provider, "opencode") {
			addOpenCodeMetadata(out, row.Metadata)
		}
	}
	return out
}

// InjectedCredentialID identifies an unambiguous inherited secret without ever
// exposing it. Explicit Agent selections are resolved before this fallback.
func (s *CredentialService) InjectedCredentialID(envKey, value string) string {
	if value == "" || s.ResolveEnv()[envKey] != value {
		return ""
	}
	var rows []models.Credential
	if s.db.Where("env_key = ? AND enabled = ? AND revoked_at IS NULL", envKey, true).Find(&rows).Error != nil {
		return ""
	}
	id := ""
	for _, row := range rows {
		if row.ValueEnc == "" {
			continue
		}
		plain, err := crypto.Decrypt(row.ValueEnc)
		if err != nil || plain != value {
			continue
		}
		if id != "" {
			return ""
		}
		id = row.ID
	}
	return id
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
	result := s.db.Model(&models.Credential{}).Where("id = ? AND value_enc = ? AND enabled = ? AND revoked_at IS NULL", row.ID, row.ValueEnc, true).Updates(map[string]any{"value_enc": enc, "updated_at": time.Now()})
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
		if row.ID == exceptID {
			continue
		}
		if credentialAliasKey(row.Name) == want {
			return strings.TrimSpace(row.Name), true, nil
		}
	}
	return "", false, nil
}

func credentialEnvRow(row models.Credential) bool {
	switch strings.ToLower(strings.TrimSpace(row.Type)) {
	case "ai", "git", "ssh", "mcp", "custom":
	default:
		return false
	}
	if isOpenCodeModelCredentialRow(row) {
		return false
	}
	key := strings.TrimSpace(row.EnvKey)
	return key != "" && !envauth.IsPlatformReservedEnvKey(key)
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
	if err != nil || strings.TrimSpace(row.ValueEnc) == "" {
		return "", "", false
	}
	dec, err := crypto.Decrypt(row.ValueEnc)
	if err != nil || strings.TrimSpace(dec) == "" {
		log.Warn().Err(err).Str("credential", row.ID).Msg("skip undecryptable workspace credential")
		return "", "", false
	}
	return strings.TrimSpace(row.EnvKey), dec, true
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
	switch NormalizeAcpBackend(backend) {
	case AcpBackendCursor:
		return builtinNameCursor
	case AcpBackendClaudeCode:
		return builtinNameClaudeCode
	case AcpBackendCodeBuddy:
		return builtinNameCodeBuddy
	case AcpBackendTrae:
		return builtinNameTrae
	case AcpBackendOpenCode:
		return builtinNameOpenCode
	case AcpBackendCodex:
		return builtinNameCodex
	case AcpBackendDeepSeekHarness:
		return builtinNameDeepSeekHarness
	default:
		backend = strings.TrimSpace(backend)
		if backend == "" {
			return "API Key"
		}
		return backend + " API Key"
	}
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

// CredentialEnvKeys returns the environment keys bound by active workspace
// credentials that carry a value. Callers use this set
// to prevent a per-run environment snapshot from shadowing a workspace
// credential binding. Empty built-in slots (materialized just by opening the
// UI) are excluded so they do not silently block Run env values.
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
func (s *CredentialService) ResolveReferences() map[string]string {
	var rows []models.Credential
	if s.db.Where("enabled = ? AND revoked_at IS NULL", true).Find(&rows).Error != nil {
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

func isOpenCodeModelCredentialRow(row models.Credential) bool {
	return strings.EqualFold(strings.TrimSpace(row.Type), "ai") && strings.EqualFold(strings.TrimSpace(row.Provider), "opencode")
}

// validateOpenCodeMetadata checks a model-vendor write. Non-opencode providers
// skip the check. required is true when the caller is storing routing metadata;
// a replace that only rotates the secret leaves metadata nil and skips this.
func validateOpenCodeMetadata(provider string, metadata map[string]any, required bool) error {
	if !strings.EqualFold(strings.TrimSpace(provider), "opencode") || !required {
		return nil
	}
	if metadata == nil {
		return ErrCredentialModel
	}
	model, _ := metadata["model"].(string)
	if strings.TrimSpace(model) == "" {
		return ErrCredentialModel
	}
	vendor, _ := metadata["provider"].(string)
	base, _ := metadata["baseUrl"].(string)
	if base == "" {
		if raw, ok := metadata["baseURL"].(string); ok {
			base = raw
		}
	}
	if strings.EqualFold(strings.TrimSpace(vendor), "custom") && strings.TrimSpace(base) == "" {
		return ErrCredentialBaseURL
	}
	return nil
}

// ResolveOpenCodeCredential returns env for the one credential id an Agent
// selected. An empty, unknown, cleared, or non-model-vendor id returns nil
// and never falls back to another key of the same kind.
func (s *CredentialService) ResolveOpenCodeCredential(credentialID string) map[string]string {
	credentialID = strings.TrimSpace(credentialID)
	if credentialID == "" || s == nil || s.db == nil {
		return nil
	}
	var row models.Credential
	err := s.db.Where("id = ? AND enabled = ? AND revoked_at IS NULL", credentialID, true).First(&row).Error
	if err != nil || !isOpenCodeModelCredentialRow(row) || strings.TrimSpace(row.ValueEnc) == "" {
		return nil
	}
	dec, err := crypto.Decrypt(row.ValueEnc)
	if err != nil || strings.TrimSpace(dec) == "" {
		log.Warn().Err(err).Str("credential", row.ID).Msg("skip undecryptable opencode credential")
		return nil
	}
	out := map[string]string{runtime.EnvGraspOpenCodeAPIKey: dec}
	if len(safeCredentialMetadata(row.Metadata)) == 0 {
		return out
	}
	provider, _ := row.Metadata["provider"].(string)
	base, _ := row.Metadata["baseUrl"].(string)
	model, _ := row.Metadata["model"].(string)
	out[runtime.EnvOpenCodeProvider] = strings.TrimSpace(provider)
	out[runtime.EnvOpenCodeBaseURL] = strings.TrimSpace(base)
	out[runtime.EnvACPBridgeModel] = strings.TrimSpace(model)
	if vision, ok := row.Metadata["vision"].(bool); ok && vision {
		out[runtime.EnvOpenCodeModelVision] = "1"
	} else {
		out[runtime.EnvOpenCodeModelVision] = ""
	}
	return out
}
