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
	// Get may reconcile provider resources (including Kubernetes Service ports).
	// Opening a channel must preserve ports published after sandbox creation.
	status, err := s.drv.Status(ctx, id)
	if err != nil {
		return "", err
	}
	if status != driver.StatusRunning {
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
