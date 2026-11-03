package services

import (
	"context"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// OpenPreviewGuest creates a separate Agent chat inside the preview's existing
// sandbox. The visitor shares the workspace but never reads the default chat.
func (s *SandboxService) OpenPreviewGuest(ctx context.Context, row models.Sandbox) (*sandbox.ACPClient, func(), error) {
	if s.mgr == nil {
		return nil, nil, errors.New("preview sandbox unavailable")
	}
	host, port, password, err := s.acpConnection(ctx, row.ID)
	if err != nil {
		return nil, nil, err
	}
	chatID, err := sandbox.CreateChat(ctx, host, port, password, "Preview visitor")
	if err != nil {
		return nil, nil, err
	}
	cwd := s.mgr.WorkspaceDir
	if s.skills != nil {
		if agent, ok := s.skills.Get(row.Profile); ok && agent.Layout.WorkspaceDir != "" {
			cwd = agent.Layout.WorkspaceDir
		}
	}
	client := sandbox.NewACPClient(host, port).WithPassword(password).WithChat(chatID).
		WithSession(cwd, nil).WithIdleTimeout(2 * time.Minute)
	closeChat := func() {
		client.Close()
		_ = sandbox.DeleteChat(context.Background(), host, port, password, chatID)
	}
	if err := client.Connect(ctx); err != nil {
		closeChat()
		return nil, nil, err
	}
	return client, closeChat, nil
}
