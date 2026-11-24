package services

import (
	"context"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// turnSteerer is a chat backend that can add input to a sandbox's running
// turn. Backends without it steer by interrupting.
type turnSteerer interface {
	Steer(ctx context.Context, id uint, text string, images []models.PromptImage) (sandbox.SteerStatus, error)
}

// SupportsSteer reports the live bridge capability for a sandbox. A missing
// or switching session is treated as unsupported until the next queue state.
func (s *SandboxService) SupportsSteer(id uint) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ls := s.live[id]
	return ls != nil && !ls.switching && ls.acp != nil && ls.acp.IsConnected() && ls.acp.SupportsSteer()
}

// Steer offers input to the turn running on the sandbox's agent. A sandbox
// that is switching tools or has no live connection has no turn to join.
func (s *SandboxService) Steer(ctx context.Context, id uint, text string, images []models.PromptImage) (sandbox.SteerStatus, error) {
	s.mu.Lock()
	ls := s.live[id]
	var acp *sandbox.ACPClient
	if ls != nil && !ls.switching {
		acp = ls.acp
	}
	s.mu.Unlock()
	if acp == nil {
		return sandbox.SteerNoTurn, nil
	}
	return acp.Steer(ctx, text, images)
}

// steerSandboxTurn delivers text into sandbox id's running turn and maps the
// bridge's answer onto the chatsession contract.
func steerSandboxTurn(ctx context.Context, backend any, id uint, text string, images []models.PromptImage) chatsession.SteerResult {
	st, ok := backend.(turnSteerer)
	if !ok || id == 0 {
		return chatsession.SteerRejected
	}
	return runtime.SteerResult(st.Steer(ctx, id, text, images))
}
