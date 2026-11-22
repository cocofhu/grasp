package services

import (
	"gorm.io/gorm"
)

// DashboardService computes the lightweight platform-status payload for the
// shell status metrics.
type DashboardService struct {
	db        *gorm.DB
	workspace *WorkspaceService
}

// NewDashboardService builds the service. workspace may be nil (Token fields stay null).
func NewDashboardService(db *gorm.DB, workspace *WorkspaceService) *DashboardService {
	return &DashboardService{db: db, workspace: workspace}
}
