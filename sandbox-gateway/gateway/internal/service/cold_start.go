package service

import (
	"context"
	"errors"

	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/models"
	"sandbox-gateway/internal/store"
)

func (s *SandboxService) StartConfigured(ctx context.Context, resource string, opts driver.StartOptions) (*driver.ColdStartReceipt, error) {
	native, ok := s.drv.(driver.ColdConfiguredStarter)
	if !ok {
		return nil, driver.ErrColdStartUnsupported
	}
	if err := driver.ValidateColdStartOptions(opts); err != nil {
		return nil, err
	}
	c := opts.ColdConfig
	// An accepted request is observed before any new lifecycle permission or
	// provider call. A retry can never execute StartConfigured a second time.
	if old, err := s.store.ColdStart(ctx, resource, c.RequestID); err == nil {
		if old.InputSHA256 != c.InputSHA256 || old.PayloadSHA256 != c.PayloadSHA256 {
			return nil, driver.ErrColdStartConflict
		}
		return s.GetColdStart(ctx, resource, c.RequestID)
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if err := s.guardEnvironmentRelease(ctx, resource); err != nil {
		return nil, err
	}
	status, err := s.drv.Status(ctx, resource)
	if err != nil {
		return nil, err
	}
	if status != driver.StatusStopped {
		return nil, driver.ErrColdStartConflict
	}
	row, admitted, err := s.store.ReserveColdStart(ctx, models.ColdStart{Resource: resource, RequestID: c.RequestID, InputSHA256: c.InputSHA256, PayloadSHA256: c.PayloadSHA256, State: "dispatching"}, c.Env)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return s.GetColdStart(ctx, resource, c.RequestID)
	}
	receipt, err := native.StartConfigured(ctx, resource, opts)
	if err != nil {
		return nil, errors.Join(driver.ErrColdStartUnknown, err)
	}
	return s.confirmColdStart(ctx, row, receipt)
}

func (s *SandboxService) PreviewColdStart(ctx context.Context, resource string) (*driver.ColdStartTarget, error) {
	native, ok := s.drv.(driver.ColdConfiguredStarter)
	if !ok {
		return nil, driver.ErrColdStartUnsupported
	}
	if err := s.guardEnvironmentRelease(ctx, resource); err != nil {
		return nil, err
	}
	if err := s.store.GuardColdStart(ctx, resource); err != nil {
		return nil, err
	}
	return native.PreviewColdStart(ctx, resource)
}

func (s *SandboxService) confirmColdStart(ctx context.Context, row models.ColdStart, receipt *driver.ColdStartReceipt) (*driver.ColdStartReceipt, error) {
	if receipt == nil || receipt.RequestID != row.RequestID || receipt.InputSHA256 != row.InputSHA256 || receipt.PayloadSHA256 != row.PayloadSHA256 || receipt.NativeIdentity == "" {
		return nil, driver.ErrColdStartConflict
	}
	if receipt.State != "started" {
		return receipt, nil
	}
	if row.NativeIdentity != "" && row.NativeIdentity != receipt.NativeIdentity {
		return nil, driver.ErrColdStartConflict
	}
	if err := s.store.ConfirmColdStart(ctx, row, receipt.NativeIdentity); err != nil {
		return nil, err
	}
	if row.State != "started" {
		go s.finalize(row.Resource)
	}
	return receipt, nil
}

// GetColdStart performs only native reads. Unknown dispatches are never replayed.
func (s *SandboxService) GetColdStart(ctx context.Context, resource, request string) (*driver.ColdStartReceipt, error) {
	native, ok := s.drv.(driver.ColdConfiguredStarter)
	if !ok {
		return nil, driver.ErrColdStartUnsupported
	}
	row, err := s.store.ColdStart(ctx, resource, request)
	if err != nil {
		return nil, err
	}
	receipt, err := native.GetColdStart(ctx, resource, request)
	if err != nil {
		return nil, errors.Join(driver.ErrColdStartUnknown, err)
	}
	return s.confirmColdStart(ctx, row, receipt)
}
