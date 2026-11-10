package services

import (
	"context"
	"fmt"

	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
)

const sandboxOperationInference = "inference"

// beginSandboxAccess fences interactive access to an already running Solo.
// Reading a history page never calls this and never starts or updates a guest.
// Callers keep release until their native request has finished. Long lived
// proxy/terminal connections remain subject to the provider's stop operation.
func (s *SandboxService) beginSandboxAccess(ctx context.Context, id uint) (context.Context, *models.Sandbox, func(), error) {
	return s.beginSandboxAccessMode(ctx, id, false)
}

// Console connections can share a fully prepared inference operation. Model,
// file and lifecycle mutations continue to require an exclusive lease.
func (s *SandboxService) beginSandboxConsumerAccess(ctx context.Context, id uint) (context.Context, *models.Sandbox, func(), error) {
	return s.beginSandboxAccessMode(ctx, id, true)
}

func (s *SandboxService) beginSandboxAccessMode(ctx context.Context, id uint, consumer bool) (context.Context, *models.Sandbox, func(), error) {
	row, err := s.Get(id)
	if err != nil {
		return ctx, nil, func() {}, err
	}
	if row.Purpose != SandboxPurposeSolo {
		return ctx, row, func() {}, nil
	}
	var release func()
	if consumer {
		ctx, release, err = s.operations().Access(ctx, id, sandboxOperationInference)
	} else {
		ctx, release, err = s.acquireSandboxOperation(ctx, id)
	}
	if err != nil {
		return ctx, nil, func() {}, err
	}
	fail := func(err error) (context.Context, *models.Sandbox, func(), error) {
		release()
		return ctx, nil, func() {}, err
	}
	row, err = s.Get(id)
	if err != nil {
		return fail(err)
	}
	if row.Status != "running" || row.DesiredState == "stopped" {
		return fail(soloErr(409, "environment_stopped", "环境尚未恢复，聊天与工作区已保留"))
	}
	grant, err := s.soloRecoveryAuthorization(ctx, row)
	if err != nil {
		return fail(soloErr(409, "environment_authorization_blocked", "当前授权未通过核验，环境访问已阻止：%s", err.Error()))
	}
	s.mu.Lock()
	live := s.live[id]
	local := live != nil && live.sb != nil && live.sb.IsLocal()
	s.mu.Unlock()
	if !local {
		if s.mgr == nil {
			return fail(fmt.Errorf("sandbox manager unavailable"))
		}
		status, probeErr := s.mgr.ProbeStatus(ctx, row.Name)
		if probeErr != nil {
			return fail(probeErr)
		}
		if status != "running" {
			return fail(soloErr(409, "environment_stopped", "环境当前未运行，请先恢复环境"))
		}
	}
	if err := s.recheckSoloRecoveryGrant(ctx, row, grant); err != nil {
		return fail(soloErr(409, "environment_authorization_blocked", "访问期间授权已变化，请重新加载"))
	}
	ctx = context.WithValue(ctx, workspaceAccessParentKey{}, workspaceAccessParent{id, grant.session.ID, grant.session.LifecycleEpoch, grant.session.LifecycleAttemptID})
	ctx, err = s.canonicalSandboxAccess(ctx, row)
	if err != nil {
		return fail(err)
	}
	if consumer {
		if err := s.operations().CommitAccess(ctx, id, func(tx *gorm.DB) error {
			return requireSoloRecoveryParent(tx, row, grant)
		}, func(*gorm.DB) error { return nil }); err != nil {
			return fail(err)
		}
	}
	return ctx, row, release, nil
}

func (s *SandboxService) soloMetadataAccessAllowed(ctx context.Context, row *models.Sandbox) bool {
	if row.Purpose != SandboxPurposeSolo {
		return true
	}
	if row.Status != "running" || row.DesiredState == "stopped" {
		return false
	}
	_, err := s.soloRecoveryAuthorization(ctx, row)
	return err == nil
}
