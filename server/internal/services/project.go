package services

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cocofhu/grasp/internal/envauth"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SecretMask is the placeholder returned for secret values on read and accepted
// on write to mean "keep the previously stored plaintext".
const SecretMask = "****"

var (
	// ErrEmptyProjectName is returned when a project name is blank.
	ErrEmptyProjectName = errors.New("项目名称不能为空")
	// ErrProjectNameExists is returned when another project already uses the name.
	ErrProjectNameExists = errors.New("项目名称已存在")
	// ErrProjectNotFound is returned when the requested project does not exist.
	ErrProjectNotFound = errors.New("project not found")
	// ErrProjectHasWorkflows is returned when deleting a project that still owns workflows.
	ErrProjectHasWorkflows = errors.New("项目下仍有流水线，请先删除全部流水线")
	// ErrSecretPlaceholderOnNewKey is returned when a new/renamed key is saved with only the mask.
	ErrSecretPlaceholderOnNewKey = errors.New("新密钥或重命名的键不能使用打码占位值，请重新填写明文")
	// ErrUnknownModelDisplayNameTooLong is returned when the alias exceeds 64 runes.
	ErrUnknownModelDisplayNameTooLong = errors.New("显示名最多 64 个字符，请缩短后再保存。")
)

// UnknownModelDisplayNameMaxLen is the max rune length for UnknownModelDisplayName.
const UnknownModelDisplayNameMaxLen = 64

// NormalizeUnknownModelDisplayName trims input; empty / whitespace-only / equal to
// the legacy or new default label become "" (unset). Over-length rejects.
func NormalizeUnknownModelDisplayName(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" || v == models.TokenUsageModelUnknown || v == models.TokenUsageModelUnknownDisplay {
		return "", nil
	}
	if utf8.RuneCountInString(v) > UnknownModelDisplayNameMaxLen {
		return "", ErrUnknownModelDisplayNameTooLong
	}
	return v, nil
}

// ResolveUnknownModelDisplayName returns the configured alias or the default display label.
func ResolveUnknownModelDisplayName(alias string) string {
	v := strings.TrimSpace(alias)
	if v == "" || v == models.TokenUsageModelUnknown || v == models.TokenUsageModelUnknownDisplay {
		return models.TokenUsageModelUnknownDisplay
	}
	return v
}

// IsConfiguredUnknownAlias reports whether alias is a real project override (not empty/default labels).
func IsConfiguredUnknownAlias(alias string) bool {
	v := strings.TrimSpace(alias)
	return v != "" && v != models.TokenUsageModelUnknown && v != models.TokenUsageModelUnknownDisplay
}

// ProjectService manages project CRUD and secret-aware config updates.
type ProjectService struct{ db *gorm.DB }

// NewProjectService builds the service.
func NewProjectService(db *gorm.DB) *ProjectService { return &ProjectService{db: db} }

// List returns all projects, newest-updated first.
func (s *ProjectService) List() []models.Project {
	var ps []models.Project
	s.db.Order("updated_at desc").Find(&ps)
	return ps
}

// Get returns one project by id.
func (s *ProjectService) Get(id string) (models.Project, bool) {
	var p models.Project
	if err := s.db.First(&p, "id = ?", id).Error; err != nil {
		return models.Project{}, false
	}
	return p, true
}

// NameExists reports whether any project (excluding excludeID) uses name.
func (s *ProjectService) NameExists(name, excludeID string) bool {
	var count int64
	q := s.db.Model(&models.Project{}).Where("name = ?", name)
	if excludeID != "" {
		q = q.Where("id != ?", excludeID)
	}
	q.Count(&count)
	return count > 0
}

// WorkflowCount returns how many workflows belong to the project.
func (s *ProjectService) WorkflowCount(projectID string) int64 {
	var n int64
	s.db.Model(&models.WorkflowDef{}).Where("project_id = ?", projectID).Count(&n)
	return n
}

// tokenAggChunk caps IN (?) size for SQLite variable limits while keeping
// TotalTokensByProjectIDs a batched (non N+1) read-path aggregation.
const tokenAggChunk = 400

// ProjectTokenBreakdown is the project-level Token card summary: workflow
// history + post-feature PM usage. Any nil field means that source has never
// reported usage (UI "—"); a non-nil 0 means reported and totals to zero.
type ProjectTokenBreakdown struct {
	Total    *int64 // workflow + pm (nil when neither source reported)
	Workflow *int64
	PM       *int64
}

// TokenBreakdown returns workflow/pm/total split for one project.
func (s *ProjectService) TokenBreakdown(projectID string) ProjectTokenBreakdown {
	return s.TokenBreakdownByProjectIDs([]string{projectID})[projectID]
}

// TokenBreakdownByProjectIDs batch-aggregates Project→WorkflowDef→Run→StateRun
// Usage plus PM ChatMessage.Usage (assistant, non-nil). Historical PM messages
// without Usage are skipped (no backfill). Stdio is outside this chain.
func (s *ProjectService) TokenBreakdownByProjectIDs(projectIDs []string) map[string]ProjectTokenBreakdown {
	out := make(map[string]ProjectTokenBreakdown, len(projectIDs))
	if len(projectIDs) == 0 {
		return out
	}
	for _, id := range projectIDs {
		out[id] = ProjectTokenBreakdown{}
	}

	wfSums, wfHas := s.sumWorkflowTokensByProjectIDs(projectIDs)
	pmSums, pmHas := s.sumPMTokensByProjectIDs(projectIDs)

	for _, pid := range projectIDs {
		b := ProjectTokenBreakdown{}
		if _, ok := wfHas[pid]; ok {
			v := wfSums[pid]
			b.Workflow = &v
		}
		if _, ok := pmHas[pid]; ok {
			v := pmSums[pid]
			b.PM = &v
		}
		if b.Workflow != nil || b.PM != nil {
			var total int64
			if b.Workflow != nil {
				total += *b.Workflow
			}
			if b.PM != nil {
				total += *b.PM
			}
			b.Total = &total
		}
		out[pid] = b
	}
	return out
}

// AggregatePlatformTokenBreakdown sums per-project breakdowns with null-aware
// semantics: Workflow/PM are sums of reported sides only (all absent → nil);
// Total is set when either side reported (nil side treated as 0 in the sum).
func AggregatePlatformTokenBreakdown(byProject map[string]ProjectTokenBreakdown) ProjectTokenBreakdown {
	var wfSum, pmSum int64
	var hasWf, hasPm bool
	for _, b := range byProject {
		if b.Workflow != nil {
			wfSum += *b.Workflow
			hasWf = true
		}
		if b.PM != nil {
			pmSum += *b.PM
			hasPm = true
		}
	}
	out := ProjectTokenBreakdown{}
	if hasWf {
		v := wfSum
		out.Workflow = &v
	}
	if hasPm {
		v := pmSum
		out.PM = &v
	}
	if hasWf || hasPm {
		var total int64
		if hasWf {
			total += wfSum
		}
		if hasPm {
			total += pmSum
		}
		out.Total = &total
	}
	return out
}

// PlatformTokenBreakdown returns the cross-project Token summary for the
// dashboard KPI (same semantics as summing each project's TokenBreakdown).
func (s *ProjectService) PlatformTokenBreakdown() ProjectTokenBreakdown {
	projects := s.List()
	if len(projects) == 0 {
		return ProjectTokenBreakdown{}
	}
	ids := make([]string, len(projects))
	for i, p := range projects {
		ids[i] = p.ID
	}
	return AggregatePlatformTokenBreakdown(s.TokenBreakdownByProjectIDs(ids))
}

func (s *ProjectService) sumWorkflowTokensByProjectIDs(projectIDs []string) (sums map[string]int64, has map[string]struct{}) {
	return s.sumLedgerTokensByProjectIDs(projectIDs, models.TokenLedgerSourceWorkflow)
}

func (s *ProjectService) sumPMTokensByProjectIDs(projectIDs []string) (sums map[string]int64, has map[string]struct{}) {
	return s.sumLedgerTokensByProjectIDs(projectIDs, models.TokenLedgerSourcePM)
}

func (s *ProjectService) sumLedgerTokensByProjectIDs(projectIDs []string, source string) (sums map[string]int64, has map[string]struct{}) {
	sums = make(map[string]int64)
	has = make(map[string]struct{})
	if len(projectIDs) == 0 {
		return sums, has
	}
	for i := 0; i < len(projectIDs); i += tokenAggChunk {
		end := i + tokenAggChunk
		if end > len(projectIDs) {
			end = len(projectIDs)
		}
		bySource, err := ledgerSumsByProjectSource(s.db, projectIDs[i:end], []string{source})
		if err != nil {
			return sums, has
		}
		for pid, m := range bySource {
			if v, ok := m[source]; ok {
				sums[pid] += v
				has[pid] = struct{}{}
			}
		}
	}
	return sums, has
}

// Create inserts a new project.
func (s *ProjectService) Create(name, description string, env []models.EnvEntry, vars []models.ProjectVariable) (models.Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return models.Project{}, ErrEmptyProjectName
	}
	if s.NameExists(name, "") {
		return models.Project{}, ErrProjectNameExists
	}
	if env == nil {
		env = []models.EnvEntry{}
	}
	if vars == nil {
		vars = []models.ProjectVariable{}
	}
	sanitizedEnv, err := sanitizeEnvEntries(env)
	if err != nil {
		return models.Project{}, err
	}
	sanitizedVars, err := sanitizeProjectVars(vars)
	if err != nil {
		return models.Project{}, err
	}
	now := time.Now()
	p := models.Project{
		ID:           "proj-" + uuid.NewString()[:8],
		Name:         name,
		Description:  description,
		SandboxEnv:   sanitizedEnv,
		Variables:    sanitizedVars,
		NotifyPolicy: models.DefaultProjectNotifyPolicy(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.db.Create(&p).Error; err != nil {
		return models.Project{}, err
	}
	return p, nil
}

// Update patches name/description/sandboxEnv/variables/notifyPolicy/
// unknownModelDisplayName. Nil pointers mean "leave unchanged"; non-nil
// slices replace the whole list with secret-preserving merge. Non-nil
// notifyPolicy replaces the whole policy. Non-nil unknownModelDisplayName is
// normalized (trim; empty/default label → unset; >64 runes → error).
func (s *ProjectService) Update(id string, name *string, description *string, env *[]models.EnvEntry, vars *[]models.ProjectVariable, notify *models.ProjectNotifyPolicy, unknownModelDisplayName *string) (models.Project, error) {
	var p models.Project
	if err := s.db.First(&p, "id = ?", id).Error; err != nil {
		return models.Project{}, ErrProjectNotFound
	}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" {
			return models.Project{}, ErrEmptyProjectName
		}
		if s.NameExists(n, id) {
			return models.Project{}, ErrProjectNameExists
		}
		p.Name = n
	}
	if description != nil {
		p.Description = *description
	}
	if env != nil {
		merged, err := mergeEnvEntries(p.SandboxEnv, *env)
		if err != nil {
			return models.Project{}, err
		}
		p.SandboxEnv = merged
	}
	if vars != nil {
		merged, err := mergeProjectVars(p.Variables, *vars)
		if err != nil {
			return models.Project{}, err
		}
		p.Variables = merged
	}
	if notify != nil {
		p.NotifyPolicy = NormalizeProjectNotifyPolicy(*notify)
	}
	if unknownModelDisplayName != nil {
		normalized, err := NormalizeUnknownModelDisplayName(*unknownModelDisplayName)
		if err != nil {
			return models.Project{}, err
		}
		p.UnknownModelDisplayName = normalized
	}
	p.UpdatedAt = time.Now()
	if err := s.db.Save(&p).Error; err != nil {
		return models.Project{}, err
	}
	return p, nil
}

// Delete removes a project when it has no workflows.
// Requirement drafts for the project are hard-deleted in the same transaction
// so they become unreachable after the project is gone.
func (s *ProjectService) Delete(id string) error {
	if _, ok := s.Get(id); !ok {
		return ErrProjectNotFound
	}
	if s.WorkflowCount(id) > 0 {
		return ErrProjectHasWorkflows
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", id).Delete(&models.RequirementDraft{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", id).Delete(&models.ProjectCredential{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.Project{}, "id = ?", id).Error
	})
}

// SandboxEnvForWorkflow returns the owning project's sandbox env (plaintext).
// Missing workflow/project yields nil (caller treats as empty).
func (s *ProjectService) SandboxEnvForWorkflow(workflowID string) []models.EnvEntry {
	var wf models.WorkflowDef
	if err := s.db.Select("project_id").First(&wf, "id = ?", workflowID).Error; err != nil || wf.ProjectID == "" {
		return nil
	}
	p, ok := s.Get(wf.ProjectID)
	if !ok {
		return nil
	}
	return p.SandboxEnv
}

// VariablesForWorkflow returns the owning project's workflow variables (plaintext).
func (s *ProjectService) VariablesForWorkflow(workflowID string) []models.ProjectVariable {
	var wf models.WorkflowDef
	if err := s.db.Select("project_id").First(&wf, "id = ?", workflowID).Error; err != nil || wf.ProjectID == "" {
		return nil
	}
	p, ok := s.Get(wf.ProjectID)
	if !ok {
		return nil
	}
	return p.Variables
}

// DefaultProjectID returns the id of「默认项目」when present, else the oldest project.
func (s *ProjectService) DefaultProjectID() string {
	var p models.Project
	if err := s.db.Where("id = ?", models.DefaultProjectID).First(&p).Error; err == nil {
		return p.ID
	}
	if err := s.db.Order("created_at asc").First(&p).Error; err == nil {
		return p.ID
	}
	return ""
}

func sanitizeEnvEntries(in []models.EnvEntry) ([]models.EnvEntry, error) {
	out := make([]models.EnvEntry, 0, len(in))
	seen := map[string]struct{}{}
	for _, e := range in {
		k := strings.TrimSpace(e.Key)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		if e.Value == SecretMask {
			return nil, ErrSecretPlaceholderOnNewKey
		}
		// Official ACP auth keys may be stored as project baseline; always force Secret.
		secret := e.Secret || envauth.IsPlatformAuthEnvKey(k)
		out = append(out, models.EnvEntry{Key: k, Value: e.Value, Secret: secret, Enabled: e.Enabled})
	}
	return out, nil
}

func sanitizeProjectVars(in []models.ProjectVariable) ([]models.ProjectVariable, error) {
	out := make([]models.ProjectVariable, 0, len(in))
	seen := map[string]struct{}{}
	for _, v := range in {
		n := strings.TrimSpace(v.Name)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		v.Name = n
		if v.Type == "" {
			v.Type = "string"
		}
		if v.Value == SecretMask {
			return nil, ErrSecretPlaceholderOnNewKey
		}
		out = append(out, v)
	}
	return out, nil
}

// mergeEnvEntries applies an incoming full-list replacement while preserving
// plaintext when the client sends empty or the mask placeholder, regardless of
// the target secret flag (so un-secreting a key cannot persist ****).
func mergeEnvEntries(existing, incoming []models.EnvEntry) ([]models.EnvEntry, error) {
	byKey := make(map[string]models.EnvEntry, len(existing))
	for _, e := range existing {
		byKey[e.Key] = e
	}
	out := make([]models.EnvEntry, 0, len(incoming))
	seen := map[string]struct{}{}
	for _, e := range incoming {
		k := strings.TrimSpace(e.Key)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		if isSecretPlaceholder(e.Value) {
			if old, ok := byKey[k]; ok {
				e.Value = old.Value
			} else if e.Value == SecretMask {
				return nil, ErrSecretPlaceholderOnNewKey
			}
		}
		// Official ACP auth keys may be stored as project baseline; always force Secret.
		secret := e.Secret || envauth.IsPlatformAuthEnvKey(k)
		out = append(out, models.EnvEntry{Key: k, Value: e.Value, Secret: secret, Enabled: e.Enabled})
	}
	return out, nil
}

func mergeProjectVars(existing, incoming []models.ProjectVariable) ([]models.ProjectVariable, error) {
	byName := make(map[string]models.ProjectVariable, len(existing))
	for _, v := range existing {
		byName[v.Name] = v
	}
	out := make([]models.ProjectVariable, 0, len(incoming))
	seen := map[string]struct{}{}
	for _, v := range incoming {
		n := strings.TrimSpace(v.Name)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		v.Name = n
		if v.Type == "" {
			v.Type = "string"
		}
		if isSecretVarPlaceholder(v.Value) {
			if old, ok := byName[n]; ok {
				v.Value = old.Value
			} else if v.Value == SecretMask {
				return nil, ErrSecretPlaceholderOnNewKey
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func isSecretPlaceholder(v string) bool {
	return v == "" || v == SecretMask
}

func isSecretVarPlaceholder(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	if !ok {
		return false
	}
	return isSecretPlaceholder(s)
}

// MaskedSandboxEnv returns env entries with secret values replaced by SecretMask.
func MaskedSandboxEnv(env []models.EnvEntry) []models.EnvEntry {
	out := make([]models.EnvEntry, len(env))
	for i, e := range env {
		out[i] = e
		if e.Secret {
			out[i].Value = SecretMask
		}
	}
	return out
}

// MaskedProjectVars returns variables with secret values replaced by SecretMask.
func MaskedProjectVars(vars []models.ProjectVariable) []models.ProjectVariable {
	out := make([]models.ProjectVariable, len(vars))
	for i, v := range vars {
		out[i] = v
		if v.Secret {
			out[i].Value = SecretMask
		}
	}
	return out
}

// ProjectEnvMap converts sandbox env entries to a key→value map for Spec.Env merge.
// Disabled entries (Enabled=false) are skipped; nil/missing Enabled counts as enabled.
func ProjectEnvMap(env []models.EnvEntry) map[string]string {
	out := make(map[string]string, len(env))
	for _, e := range env {
		if e.Key == "" || !e.IsEnabled() {
			continue
		}
		out[e.Key] = e.Value
	}
	return out
}

// FormatProjectHasWorkflowsError returns a stable API error string.
func FormatProjectHasWorkflowsError(n int64) string {
	return fmt.Sprintf("%s（%d）", ErrProjectHasWorkflows.Error(), n)
}
