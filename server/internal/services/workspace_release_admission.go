package services

import (
	"context"

	"gorm.io/gorm"
)

func (s *SandboxService) guardWorkspaceReleaseTx(ctx context.Context, tx *gorm.DB, id uint) error {
	if s.workspaceReleaseGuard != nil {
		return s.workspaceReleaseGuard(ctx, tx, id)
	}
	return nil
}

func (s *SandboxService) checkWorkspaceRelease(ctx context.Context, id uint) error {
	if s.workspaceReleaseGuard == nil {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return s.guardWorkspaceReleaseTx(ctx, tx, id) })
}
