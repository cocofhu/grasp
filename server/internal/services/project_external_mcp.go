package services

import (
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

// ProjectExternalMcpSettingsView is the REST shape for external MCP settings.
type ProjectExternalMcpSettingsView struct {
	Enabled      bool      `json:"enabled"`
	EnabledPacks []string  `json:"enabledPacks"`
	McpBaseURL   string    `json:"mcpBaseUrl,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
}

// ProjectExternalMcpService manages external MCP settings per project.
type ProjectExternalMcpService struct {
	db *gorm.DB
}

// NewProjectExternalMcpService builds the service.
func NewProjectExternalMcpService(db *gorm.DB, _ string) *ProjectExternalMcpService {
	return &ProjectExternalMcpService{db: db}
}

// Get returns settings for a project, creating defaults when missing.
func (s *ProjectExternalMcpService) Get(projectID string) (ProjectExternalMcpSettingsView, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ProjectExternalMcpSettingsView{}, errors.New("project id required")
	}
	var row models.ProjectExternalMcpSettings
	err := s.db.First(&row, "project_id = ?", projectID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ProjectExternalMcpSettingsView{
			Enabled:      false,
			EnabledPacks: []string{},
			McpBaseURL:   s.baseURL(projectID),
		}, nil
	}
	if err != nil {
		return ProjectExternalMcpSettingsView{}, err
	}
	packs := row.EnabledPacks
	if packs == nil {
		packs = []string{}
	}
	return ProjectExternalMcpSettingsView{
		Enabled:      row.Enabled,
		EnabledPacks: packs,
		McpBaseURL:   s.baseURL(projectID),
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

// Update persists enabled + enabledPacks. Project must exist.
func (s *ProjectExternalMcpService) Update(projectID string, enabled bool, enabledPacks []string) (ProjectExternalMcpSettingsView, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ProjectExternalMcpSettingsView{}, errors.New("project id required")
	}
	var proj models.Project
	if err := s.db.Select("id").First(&proj, "id = ?", projectID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ProjectExternalMcpSettingsView{}, ErrProjectNotFound
		}
		return ProjectExternalMcpSettingsView{}, err
	}
	filtered := FilterPmEnabledMcps(enabledPacks)
	now := time.Now()
	row := models.ProjectExternalMcpSettings{
		ProjectID:    projectID,
		Enabled:      enabled,
		EnabledPacks: filtered,
		UpdatedAt:    now,
	}
	if err := s.db.Save(&row).Error; err != nil {
		return ProjectExternalMcpSettingsView{}, err
	}
	return ProjectExternalMcpSettingsView{
		Enabled:      row.Enabled,
		EnabledPacks: append([]string{}, filtered...),
		McpBaseURL:   s.baseURL(projectID),
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

// IsEnabled reports whether external MCP is turned on for the project.
func (s *ProjectExternalMcpService) IsEnabled(projectID string) bool {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return false
	}
	var row models.ProjectExternalMcpSettings
	if err := s.db.Select("enabled").First(&row, "project_id = ?", projectID).Error; err != nil {
		return false
	}
	return row.Enabled
}

// EnabledPacks returns the configured pack list (may be empty).
func (s *ProjectExternalMcpService) EnabledPacks(projectID string) []string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil
	}
	var row models.ProjectExternalMcpSettings
	if err := s.db.Select("enabled_packs").First(&row, "project_id = ?", projectID).Error; err != nil {
		return []string{}
	}
	if row.EnabledPacks == nil {
		return []string{}
	}
	return append([]string{}, row.EnabledPacks...)
}

func (s *ProjectExternalMcpService) baseURL(projectID string) string {
	base := strings.TrimRight(config.EffectiveMCPAdvertise(), "/")
	if base == "" {
		return "/mcp/external/" + projectID
	}
	return base + "/mcp/external/" + projectID
}
