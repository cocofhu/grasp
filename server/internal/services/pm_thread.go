package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// --- chat threads / messages ------------------------------------------------

// ListThreads returns the user's own threads, ordered by updated_at desc.
func (s *PmService) ListThreads(userID string) ([]models.ChatThread, error) {
	var threads []models.ChatThread
	if err := s.db.Where("user_id = ?", userID).Order("updated_at desc").Find(&threads).Error; err != nil {
		return nil, err
	}
	return threads, nil
}

// CreateThread inserts a new private thread for the user.
// agentName scopes the thread for context-store isolation; kind defaults to user.
func (s *PmService) CreateThread(userID, title, agentName, kind string) (models.ChatThread, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "新会话"
	}
	if kind == "" {
		kind = models.ChatThreadKindUser
	}
	if agentName == "" {
		if ws, err := loadWorkspace(s.db); err == nil {
			agentName = ws.PmLeaderAgent
		}
	}
	now := time.Now()
	t := models.ChatThread{
		ID: "thr-" + uuid.NewString()[:12], UserID: userID,
		AgentName: agentName, Kind: kind, Title: title, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.Create(&t).Error; err != nil {
		return models.ChatThread{}, err
	}
	return t, nil
}

// GetThread returns a thread owned by userID.
func (s *PmService) GetThread(threadID, userID string) (models.ChatThread, error) {
	var t models.ChatThread
	if err := s.db.Where("id = ?", threadID).First(&t).Error; err != nil {
		return models.ChatThread{}, ErrPmThreadNotFound
	}
	if t.UserID == userID {
		return t, nil
	}
	return models.ChatThread{}, ErrPmThreadNotFound
}

// GetThreadByID loads a thread without user check (PM MCP internal).
func (s *PmService) GetThreadByID(threadID string) (models.ChatThread, error) {
	var t models.ChatThread
	if err := s.db.Where("id = ?", threadID).First(&t).Error; err != nil {
		return models.ChatThread{}, ErrPmThreadNotFound
	}
	return t, nil
}

// BindSandbox stores the sandbox id on the thread.
func (s *PmService) BindSandbox(threadID string, sandboxID uint) error {
	ref := fmt.Sprintf("%d", sandboxID)
	return s.db.Model(&models.ChatThread{}).Where("id = ?", threadID).
		Updates(map[string]any{"sandbox_ref": ref, "updated_at": time.Now()}).Error
}

// ClearSandboxRef clears a dead sandbox binding.
func (s *PmService) ClearSandboxRef(threadID string) error {
	return s.db.Model(&models.ChatThread{}).Where("id = ?", threadID).
		Updates(map[string]any{"sandbox_ref": "", "updated_at": time.Now()}).Error
}

// ListConversationsForAgent returns threads visible to a context-store session:
// the caller's own user threads for the agent (all of them when userID is empty).
func (s *PmService) ListConversationsForAgent(agentName, userID string) ([]models.ChatThread, error) {
	q := s.db.Where("agent_name = ?", agentName)
	if userID != "" {
		q = q.Where("(kind = ? OR kind = '' OR kind IS NULL) AND user_id = ?", models.ChatThreadKindUser, userID)
	}
	var threads []models.ChatThread
	if err := q.Order("updated_at desc").Find(&threads).Error; err != nil {
		return nil, err
	}
	return threads, nil
}

// DeleteThread removes a thread and its messages (owner only).
func (s *PmService) DeleteThread(threadID, userID string) error {
	if _, err := s.GetThread(threadID, userID); err != nil {
		return err
	}
	return s.DeleteThreadByID(threadID)
}

// DeleteThreadByID removes a thread and its messages without an ownership check.
func (s *PmService) DeleteThreadByID(threadID string) error {
	if strings.TrimSpace(threadID) == "" {
		return nil
	}
	_ = s.db.Where("thread_id = ?", threadID).Delete(&models.ChatTurnDraft{}).Error
	if err := s.db.Where("thread_id = ?", threadID).Delete(&models.ChatMessage{}).Error; err != nil {
		return err
	}
	return s.db.Where("id = ?", threadID).Delete(&models.ChatThread{}).Error
}

// GetThreadForAgent returns one thread when it belongs to agent.
func (s *PmService) GetThreadForAgent(agentName, threadID string) (models.ChatThread, error) {
	agentName = strings.TrimSpace(agentName)
	var t models.ChatThread
	if err := s.db.Where("id = ? AND agent_name = ?", threadID, agentName).
		First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.ChatThread{}, ErrPmThreadNotFound
		}
		return models.ChatThread{}, err
	}
	return t, nil
}

// ListThreadsForAgent returns every thread (any owner/kind) for an agent.
// Used by Agent Studio context management (not the per-user consult sidebar).
func (s *PmService) ListThreadsForAgent(agentName string) ([]models.ChatThread, error) {
	agentName = strings.TrimSpace(agentName)
	var threads []models.ChatThread
	if err := s.db.Where("agent_name = ?", agentName).
		Order("updated_at desc").Find(&threads).Error; err != nil {
		return nil, err
	}
	if threads == nil {
		threads = []models.ChatThread{}
	}
	return threads, nil
}

// DeleteThreadForAgent deletes a thread only when it belongs to agent.
func (s *PmService) DeleteThreadForAgent(agentName, threadID string) error {
	agentName = strings.TrimSpace(agentName)
	threadID = strings.TrimSpace(threadID)
	var t models.ChatThread
	if err := s.db.Where("id = ? AND agent_name = ?", threadID, agentName).
		First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPmThreadNotFound
		}
		return err
	}
	return s.DeleteThreadByID(threadID)
}

// Draft status constants.
const (
	PmDraftStreaming = "streaming"
	PmDraftDone      = "done"
	PmDraftFailed    = "failed"
)

// GetDraft returns the active draft for a thread, or nil when absent.
func (s *PmService) GetDraft(threadID string) (*models.ChatTurnDraft, error) {
	var d models.ChatTurnDraft
	err := s.db.Where("thread_id = ?", threadID).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// UpsertDraft creates or updates the per-thread streaming checkpoint.
func (s *PmService) UpsertDraft(threadID, userMsgID, partialText, status string, chunkIndex, eventSeq int, sandboxID uint) (models.ChatTurnDraft, error) {
	if status == "" {
		status = PmDraftStreaming
	}
	now := time.Now()
	existing, err := s.GetDraft(threadID)
	if err != nil {
		return models.ChatTurnDraft{}, err
	}
	if existing == nil {
		d := models.ChatTurnDraft{
			ID:          "draft-" + uuid.NewString()[:12],
			ThreadID:    threadID,
			UserMsgID:   userMsgID,
			PartialText: partialText,
			ChunkIndex:  chunkIndex,
			EventSeq:    eventSeq,
			Status:      status,
			SandboxID:   sandboxID,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := s.db.Create(&d).Error; err != nil {
			return models.ChatTurnDraft{}, err
		}
		return d, nil
	}
	updates := map[string]any{
		"user_msg_id":  userMsgID,
		"partial_text": partialText,
		"chunk_index":  chunkIndex,
		"event_seq":    eventSeq,
		"status":       status,
		"sandbox_id":   sandboxID,
		"updated_at":   now,
		"fail_kind":    "",
	}
	if err := s.db.Model(&models.ChatTurnDraft{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
		return models.ChatTurnDraft{}, err
	}
	existing.UserMsgID = userMsgID
	existing.PartialText = partialText
	existing.ChunkIndex = chunkIndex
	existing.EventSeq = eventSeq
	existing.Status = status
	existing.SandboxID = sandboxID
	existing.FailKind = ""
	existing.UpdatedAt = now
	return *existing, nil
}

// FailDraft marks the draft failed (keeps partial for hydrate diagnostics).
func (s *PmService) FailDraft(threadID, failKind string) error {
	if failKind == "" {
		failKind = PmFailUnknown
	}
	return s.db.Model(&models.ChatTurnDraft{}).Where("thread_id = ?", threadID).
		Updates(map[string]any{
			"status":     PmDraftFailed,
			"fail_kind":  failKind,
			"updated_at": time.Now(),
		}).Error
}

// ClearDraft removes the thread draft after finalize or discard.
func (s *PmService) ClearDraft(threadID string) error {
	return s.db.Where("thread_id = ?", threadID).Delete(&models.ChatTurnDraft{}).Error
}

// HasAssistantAfter reports whether an assistant message exists after the given user message.
func (s *PmService) HasAssistantAfter(threadID, userMsgID string) (bool, error) {
	var user models.ChatMessage
	if err := s.db.Where("id = ? AND thread_id = ?", userMsgID, threadID).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	var count int64
	err := s.db.Model(&models.ChatMessage{}).
		Where("thread_id = ? AND role = ? AND created_at >= ?", threadID, "assistant", user.CreatedAt).
		Count(&count).Error
	return count > 0, err
}

// FailInterruptedTurns fails every turn left streaming by a previous process:
// its runner is gone, so the user message gets failKind interrupted and can be
// retried. Call once at boot before any turn starts.
func (s *PmService) FailInterruptedTurns() (int, error) {
	var drafts []models.ChatTurnDraft
	if err := s.db.Where("status = ?", PmDraftStreaming).Find(&drafts).Error; err != nil {
		return 0, err
	}
	for _, d := range drafts {
		if d.UserMsgID != "" {
			if _, err := s.UpdateMessageFailure(d.ThreadID, d.UserMsgID, "failed", PmFailInterrupted); err != nil {
				log.Warn().Err(err).Str("thread", d.ThreadID).Msg("pm interrupted turn mark failed")
			}
		}
		if err := s.FailDraft(d.ThreadID, PmFailInterrupted); err != nil {
			return 0, err
		}
	}
	return len(drafts), nil
}
