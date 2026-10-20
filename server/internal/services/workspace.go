package services

import (
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

var (
	// ErrWorkspaceMissing means the singleton workspace row is missing; the
	// database was not initialized by this server version.
	ErrWorkspaceMissing = errors.New("workspace not initialized")
	// ErrEmptyWorkspaceName is returned when the workspace name is blank.
	ErrEmptyWorkspaceName = errors.New("工作区名称不能为空")
)

// WorkspaceService reads and updates the singleton workspace configuration.
type WorkspaceService struct{ db *gorm.DB }

// NewWorkspaceService builds the service.
func NewWorkspaceService(db *gorm.DB) *WorkspaceService { return &WorkspaceService{db: db} }

func loadWorkspace(db *gorm.DB) (models.Workspace, error) {
	var p models.Workspace
	if err := db.First(&p, "id = ?", models.WorkspaceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Workspace{}, ErrWorkspaceMissing
		}
		return models.Workspace{}, err
	}
	return p, nil
}

func workspaceRow(db *gorm.DB) *gorm.DB {
	return db.Model(&models.Workspace{}).Where("id = ?", models.WorkspaceID)
}

// Workspace returns the workspace configuration.
func (s *WorkspaceService) Workspace() (models.Workspace, error) {
	if s == nil || s.db == nil {
		return models.Workspace{}, ErrWorkspaceMissing
	}
	return loadWorkspace(s.db)
}

// Update patches name/description/variables/notifyPolicy/
// unknownModelDisplayName. Nil pointers mean "leave unchanged"; non-nil
// slices replace the whole list with secret-preserving merge. Non-nil
// notifyPolicy replaces the whole policy. Non-nil unknownModelDisplayName is
// normalized (trim; empty/default label → unset; >64 runes → error).
func (s *WorkspaceService) Update(name *string, description *string, vars *[]models.WorkspaceVariable, notify *models.WorkspaceNotifyPolicy, unknownModelDisplayName *string) (models.Workspace, error) {
	p, err := loadWorkspace(s.db)
	if err != nil {
		return models.Workspace{}, err
	}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" {
			return models.Workspace{}, ErrEmptyWorkspaceName
		}
		p.Name = n
	}
	if description != nil {
		p.Description = *description
	}
	if vars != nil {
		merged, err := mergeWorkspaceVars(p.Variables, *vars)
		if err != nil {
			return models.Workspace{}, err
		}
		p.Variables = merged
	}
	if notify != nil {
		p.NotifyPolicy = NormalizeWorkspaceNotifyPolicy(*notify)
	}
	if unknownModelDisplayName != nil {
		normalized, err := NormalizeUnknownModelDisplayName(*unknownModelDisplayName)
		if err != nil {
			return models.Workspace{}, err
		}
		p.UnknownModelDisplayName = normalized
	}
	p.UpdatedAt = time.Now()
	if err := s.db.Save(&p).Error; err != nil {
		return models.Workspace{}, err
	}
	return p, nil
}

// VariablesForWorkflow returns the effective workflow variables (plaintext):
// workspace variables when inherited, overridden by the workflow's own.
func (s *WorkspaceService) VariablesForWorkflow(workflowID string) []models.WorkspaceVariable {
	var wf models.WorkflowDef
	if err := s.db.Select("id", "defaults").First(&wf, "id = ?", workflowID).Error; err != nil {
		return nil
	}
	p, err := loadWorkspace(s.db)
	if err != nil {
		return nil
	}
	if wf.Defaults == nil {
		return p.Variables
	}
	vars := []models.WorkspaceVariable{}
	if wf.Defaults.InheritWorkspace {
		vars = append(vars, p.Variables...)
	}
	byName := map[string]int{}
	for i, v := range vars {
		byName[v.Name] = i
	}
	for _, v := range wf.Defaults.Variables {
		if i, ok := byName[v.Name]; ok {
			vars[i] = v
		} else {
			byName[v.Name] = len(vars)
			vars = append(vars, v)
		}
	}
	return vars
}

// WorkspaceTokenBreakdown is the workspace Token card summary: workflow
// history + PM usage. Any nil field means that source has never reported
// usage (UI "—"); a non-nil 0 means reported and totals to zero.
type WorkspaceTokenBreakdown struct {
	Total    *int64 // workflow + pm (nil when neither source reported)
	Workflow *int64
	PM       *int64
}

// TokenBreakdown sums the workflow and PM ledger sources.
func (s *WorkspaceService) TokenBreakdown() WorkspaceTokenBreakdown {
	type row struct {
		Source string
		Total  int64
	}
	var rows []row
	s.db.Model(&models.TokenUsageEvent{}).
		Select("source, SUM(input_tokens + output_tokens + cache_read_tokens + cache_write_tokens) AS total").
		Where("source IN ?", []string{models.TokenLedgerSourceWorkflow, models.TokenLedgerSourcePM}).
		Group("source").Scan(&rows)
	var out WorkspaceTokenBreakdown
	for _, r := range rows {
		v := r.Total
		if r.Source == models.TokenLedgerSourcePM {
			out.PM = &v
		} else {
			out.Workflow = &v
		}
		if out.Total == nil {
			out.Total = new(int64)
		}
		*out.Total += v
	}
	return out
}

// WorkflowCount returns how many workflows exist.
func (s *WorkspaceService) WorkflowCount() int64 {
	var n int64
	s.db.Model(&models.WorkflowDef{}).Count(&n)
	return n
}
