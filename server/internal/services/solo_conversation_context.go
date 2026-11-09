package services

import (
	"context"
	"fmt"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
)

type soloConversationIdentity struct {
	sandboxID   uint
	native      string
	workspace   string
	fingerprint string
	// Old runtimes have no stable native-context identity. Only in that case
	// a new connected client conservatively requires another history preamble.
	client *sandbox.ACPClient
}

type soloConversationPreparer interface {
	prepareSoloConversation(context.Context, uint) (soloConversationIdentity, error)
}

// Establish the same connection ChatWithTimeout will use before deciding
// whether its prompt needs persisted conversation context. This sends no
// prompt, resets no Agent/model and changes no environment. NativeSessionID is
// the backend-instance/Agent-generation identity, not a vendor thread display,
// request nonce, grant or SQL physical-operation generation.
func (s *SandboxService) prepareSoloConversation(ctx context.Context, id uint) (soloConversationIdentity, error) {
	live, row, err := s.ensureConnected(ctx, id)
	if err != nil {
		return soloConversationIdentity{}, err
	}
	if row.Purpose != SandboxPurposeSolo || live.acp == nil || !live.acp.IsConnected() {
		return soloConversationIdentity{}, fmt.Errorf("Solo conversation connection is unavailable")
	}
	if live.acp.NativeSessionID() != "" {
		// HTTP model settings can replace a native Agent without replacing its
		// websocket. Refresh the existing read-only identity protocol first.
		check, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := live.acp.RefreshOperationSession(check)
		cancel()
		if err != nil {
			return soloConversationIdentity{}, err
		}
	}
	workspace, fingerprint := live.acp.WorkspaceIdentity()
	identity := soloConversationIdentity{sandboxID: id, native: live.acp.NativeSessionID(), workspace: workspace, fingerprint: fingerprint}
	if identity.native == "" {
		identity.client = live.acp
	}
	return identity, nil
}
