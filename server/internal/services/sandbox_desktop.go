package services

import (
	"context"

	"github.com/cocofhu/grasp/internal/sandbox"
)

// DesktopStatus proxies the sandbox desktop status.
func (s *SandboxService) DesktopStatus(ctx context.Context, id uint) (sandbox.DesktopStatus, error) {
	host, port, password, err := s.acpConnection(ctx, id)
	if err != nil {
		return sandbox.DesktopStatus{}, err
	}
	return sandbox.GetDesktopStatus(ctx, host, port, password)
}

// SetDesktopControl takes (holder=user) or returns (holder=agent) the
// sandbox desktop for one viewer. Callers check the user may access it.
func (s *SandboxService) SetDesktopControl(ctx context.Context, id uint, holder, client string) (sandbox.DesktopStatus, error) {
	host, port, password, err := s.acpConnection(ctx, id)
	if err != nil {
		return sandbox.DesktopStatus{}, err
	}
	return sandbox.SetDesktopControl(ctx, host, port, password, holder, client)
}

// SetDesktopClipboard sends text to the desktop on the sandbox session channel.
func (s *SandboxService) SetDesktopClipboard(ctx context.Context, id uint, text string) error {
	host, port, password, err := s.acpConnection(ctx, id)
	if err != nil {
		return err
	}
	return sandbox.SetDesktopClipboard(ctx, host, port, password, text)
}
