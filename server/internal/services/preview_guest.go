package services

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/chatcmd"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

func (s *SandboxService) PreviewGuestSkillPrompt(row models.Sandbox, name, prompt string) (string, error) {
	if s.skills == nil {
		return "", chatcmd.UnknownSkill(name)
	}
	agent, ok := s.skills.Get(row.Profile)
	if !ok {
		return "", chatcmd.UnknownSkill(name)
	}
	var home, root string
	s.mu.Lock()
	if live := s.live[row.ID]; live != nil {
		home = live.home
		if live.sb != nil {
			root = live.sb.ConfigRoot
		}
	}
	s.mu.Unlock()
	if home != "" {
		if !chatcmd.InstalledSkill(name, home) {
			return "", chatcmd.UnknownSkill(name)
		}
	} else if !slices.Contains(soloSkills(s.effectiveAgent(agent).Files), name) {
		return "", chatcmd.UnknownSkill(name)
	}
	if root == "" {
		root = s.effectiveAgent(agent).Layout.ConfigRoot
	}
	return chatcmd.WithSkill(root, name, prompt), nil
}

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

// PreviewGuestExecution is the route of the next visitor turn: the preview's
// Agent on the model the visitor chat has selected.
func (s *SandboxService) PreviewGuestExecution(ctx context.Context, row models.Sandbox, chatID string) *models.ChatExecution {
	execution := &models.ChatExecution{AgentName: row.Profile}
	if s.skills != nil {
		if agent, ok := s.skills.Get(row.Profile); ok {
			execution.Backend = agent.AcpBackend
		}
	}
	if current, err := s.ChatModelSettings(ctx, row.ID, chatID); err == nil {
		execution.ModelID = current.Selected
		if execution.ModelID == "" {
			execution.ModelID = current.Default
		}
	}
	return execution
}

// ChatPreviewGuestTurn runs one visitor turn with transient attachment storage
// and ledgers it against the preview's owner (runID/nodeID of the share link).
func (s *SandboxService) ChatPreviewGuestTurn(ctx context.Context, row models.Sandbox, runID, nodeID string, client *sandbox.ACPClient, prompt string, files []models.PromptImage, onProgress func(*sandbox.ChatResult)) (*sandbox.ChatResult, error) {
	if len(files) > 0 {
		store := blob.NewMemory()
		var err error
		files, err = blob.IngestPromptImages(ctx, store, files)
		if err != nil {
			return nil, err
		}
		client.WithBlobs(store)
		defer client.WithBlobs(nil)
	}
	result, err := client.ChatStreamResult(ctx, prompt, files, onProgress)
	recordTurnUsage(s.db, previewGuestUsage(&row, runID, nodeID), result, err)
	return result, err
}
