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

// Update patches name/description/notifyPolicy/unknownModelDisplayName.
// Nil pointers mean "leave unchanged". Non-nil notifyPolicy replaces the whole policy. Non-nil unknownModelDisplayName is
// normalized (trim; empty/default label → unset; >64 runes → error).
func (s *WorkspaceService) Update(name *string, description *string, notify *models.WorkspaceNotifyPolicy, unknownModelDisplayName *string) (models.Workspace, error) {
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

// VariablesForWorkflow returns the workflow's own default variables (plaintext).
func (s *WorkspaceService) VariablesForWorkflow(workflowID string) []models.WorkspaceVariable {
	var wf models.WorkflowDef
	if err := s.db.Select("id", "defaults").First(&wf, "id = ?", workflowID).Error; err != nil || wf.Defaults == nil {
		return nil
	}
	return wf.Defaults.Variables
}

// WorkspaceTokenBreakdown summarizes all ledger usage with workflow and PM
// subtotals. A nil field means no usage was reported for that scope (UI "—");
// a non-nil 0 means reported and totals to zero.
type WorkspaceTokenBreakdown struct {
	Total    *int64 // All sources (nil when no source reported).
	Workflow *int64
	PM       *int64
}

// TokenBreakdown sums every ledger source, matching GlobalTokenStats.
func (s *WorkspaceService) TokenBreakdown() WorkspaceTokenBreakdown {
	type row struct {
		Source string
		Total  int64
	}
	var rows []row
	s.db.Model(&models.TokenUsageEvent{}).
		Select("source, SUM(" + ledgerTotalSQL + ") AS total").
		Group("source").Scan(&rows)
	var out WorkspaceTokenBreakdown
	for _, r := range rows {
		v := r.Total
		switch r.Source {
		case models.TokenLedgerSourceWorkflow:
			out.Workflow = &v
		case models.TokenLedgerSourcePM:
			out.PM = &v
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
