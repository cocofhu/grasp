package service

import (
	"context"
	"fmt"

	"sandbox-gateway/internal/driver"
)

// ConnectionTarget resolves current provider endpoints rather than trusting
// persisted host addresses after a pod reschedule or an E2B relay restart.
func (s *SandboxService) ConnectionTarget(ctx context.Context, id string, port int) (string, error) {
	if _, err := s.store.Get(ctx, id); err != nil {
		return "", err
	}
	handle, err := s.drv.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if handle.Status != driver.StatusRunning {
		return "", fmt.Errorf("sandbox is not running")
	}
	eps, err := s.drv.Endpoints(ctx, id)
	if err != nil {
		return "", err
	}
	if addr := eps[port]; addr != "" {
		return addr, nil
	}
	return "", fmt.Errorf("%w %d", ErrEndpointNotFound, port)
}
