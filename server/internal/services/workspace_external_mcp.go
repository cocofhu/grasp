package services

import (
	"errors"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"

	"gorm.io/gorm"
)

// ExternalMcpSettingsView is the REST shape for external MCP settings.
type ExternalMcpSettingsView struct {
	Enabled      bool      `json:"enabled"`
	EnabledPacks []string  `json:"enabledPacks"`
	McpBaseURL   string    `json:"mcpBaseUrl,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
}

// ExternalMcpService manages the workspace external MCP settings.
type ExternalMcpService struct {
	db *gorm.DB
}

// NewExternalMcpService builds the service.
func NewExternalMcpService(db *gorm.DB) *ExternalMcpService {
	return &ExternalMcpService{db: db}
}

// Get returns the settings, or defaults when none are saved.
func (s *ExternalMcpService) Get() (ExternalMcpSettingsView, error) {
	var row models.ExternalMcpSettings
	err := s.db.First(&row, "id = ?", models.WorkspaceID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ExternalMcpSettingsView{
			Enabled:      false,
			EnabledPacks: []string{},
			McpBaseURL:   s.baseURL(),
		}, nil
	}
	if err != nil {
		return ExternalMcpSettingsView{}, err
	}
	packs := row.EnabledPacks
	if packs == nil {
		packs = []string{}
	}
	return ExternalMcpSettingsView{
		Enabled:      row.Enabled,
		EnabledPacks: packs,
		McpBaseURL:   s.baseURL(),
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

// Update persists enabled + enabledPacks.
func (s *ExternalMcpService) Update(enabled bool, enabledPacks []string) (ExternalMcpSettingsView, error) {
	filtered := FilterPmEnabledMcps(enabledPacks)
	now := time.Now()
	row := models.ExternalMcpSettings{
		ID:           models.WorkspaceID,
		Enabled:      enabled,
		EnabledPacks: filtered,
		UpdatedAt:    now,
	}
	if err := s.db.Save(&row).Error; err != nil {
		return ExternalMcpSettingsView{}, err
	}
	return ExternalMcpSettingsView{
		Enabled:      row.Enabled,
		EnabledPacks: append([]string{}, filtered...),
		McpBaseURL:   s.baseURL(),
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

// IsEnabled reports whether external MCP is turned on.
func (s *ExternalMcpService) IsEnabled() bool {
	var row models.ExternalMcpSettings
	if err := s.db.Select("enabled").First(&row, "id = ?", models.WorkspaceID).Error; err != nil {
		return false
	}
	return row.Enabled
}

// EnabledPacks returns the configured pack list (may be empty).
func (s *ExternalMcpService) EnabledPacks() []string {
	var row models.ExternalMcpSettings
	if err := s.db.Select("enabled_packs").First(&row, "id = ?", models.WorkspaceID).Error; err != nil {
		return []string{}
	}
	if row.EnabledPacks == nil {
		return []string{}
	}
	return append([]string{}, row.EnabledPacks...)
}

func (s *ExternalMcpService) baseURL() string {
	base := strings.TrimRight(config.EffectiveMCPAdvertise(), "/")
	if base == "" {
		return "/mcp/workspace"
	}
	return base + "/mcp/workspace"
}
