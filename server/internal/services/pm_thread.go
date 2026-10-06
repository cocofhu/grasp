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

// ListThreads returns threads for the given user in the project.
// For normal Web users this is their own threads ∪ project channel threads
// (registered type prefixes), excluding cron: threads, ordered by updated_at desc.
// For synthetic identities (qq:/wecom:/feishu:/cron:) it stays owner-only so
// ChannelBridge ensureThread continues to resolve a single conversation thread.
func (s *PmService) ListThreads(projectID, userID string) ([]models.ChatThread, error) {
	if _, ok := s.project(projectID); !ok {
		return nil, ErrProjectNotFound
	}
	var threads []models.ChatThread
	q := s.db.Where("project_id = ?", projectID)
	if IsSyntheticThreadUserID(userID) {
		q = q.Where("user_id = ?", userID)
	} else {
		// Own Web sessions ∪ registered channel sessions; cron: never matches.
		q = q.Where("user_id = ? OR "+channelUserIDLikeSQL(), append([]any{userID}, channelUserIDLikeArgs()...)...)
	}
	if err := q.Order("updated_at desc").Find(&threads).Error; err != nil {
		return nil, err
	}
	s.annotateUnspoken(threads)
	return threads, nil
}

func channelUserIDLikeSQL() string {
	parts := make([]string, 0, len(models.RegisteredChannelTypes()))
	for range models.RegisteredChannelTypes() {
		parts = append(parts, "user_id LIKE ?")
	}
	return strings.Join(parts, " OR ")
}

func channelUserIDLikeArgs() []any {
	args := make([]any, 0, len(models.RegisteredChannelTypes()))
	for _, typ := range models.RegisteredChannelTypes() {
		args = append(args, typ+":%")
	}
	return args
}

// HasChannelInbound reports whether a synthetic channel identity has at least
// one persisted source=channel inbound message in this project.
func (s *PmService) HasChannelInbound(projectID, syntheticUserID string) bool {
	projectID = strings.TrimSpace(projectID)
	syntheticUserID = strings.TrimSpace(syntheticUserID)
	if projectID == "" || syntheticUserID == "" || !IsChannelUserID(syntheticUserID) {
		return false
	}
	var threadIDs []string
	if err := s.db.Model(&models.ChatThread{}).
		Where("project_id = ? AND user_id = ?", projectID, syntheticUserID).
		Pluck("id", &threadIDs).Error; err != nil || len(threadIDs) == 0 {
		return false
	}
	var n int64
	if err := s.db.Model(&models.ChatMessage{}).
		Where("thread_id IN ? AND source = ? AND role = ?", threadIDs, "channel", "user").
		Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}

func (s *PmService) annotateUnspoken(threads []models.ChatThread) {
	ids := make([]string, 0, len(threads))
	for _, th := range threads {
		if IsChannelUserID(th.UserID) {
			ids = append(ids, th.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	var spokenIDs []string
	_ = s.db.Model(&models.ChatMessage{}).
		Where("thread_id IN ? AND source = ? AND role = ?", ids, "channel", "user").
		Distinct("thread_id").Pluck("thread_id", &spokenIDs).Error
	spoken := map[string]bool{}
	for _, id := range spokenIDs {
		spoken[id] = true
	}
	for i := range threads {
		if IsChannelUserID(threads[i].UserID) && !spoken[threads[i].ID] {
			threads[i].Unspoken = true
		}
	}
}

// CreateThread inserts a new private thread for the user.
// agentName scopes the thread for context-store isolation; kind defaults to user.
func (s *PmService) CreateThread(projectID, userID, title, agentName, kind string) (models.ChatThread, error) {
	if _, ok := s.project(projectID); !ok {
		return models.ChatThread{}, ErrProjectNotFound
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "新会话"
	}
	if kind == "" {
		kind = models.ChatThreadKindUser
	}
	if agentName == "" {
		if p, ok := s.project(projectID); ok {
			agentName = p.PmLeaderAgent
		}
	}
	now := time.Now()
	t := models.ChatThread{
		ID: "thr-" + uuid.NewString()[:12], ProjectID: projectID, UserID: userID,
		AgentName: agentName, Kind: kind, Title: title, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.Create(&t).Error; err != nil {
		return models.ChatThread{}, err
	}
	return t, nil
}

// CreateCronThread creates an exclusive kind=cron thread for a scheduled job.
func (s *PmService) CreateCronThread(projectID, agentName, title string) (models.ChatThread, error) {
	return s.CreateThread(projectID, "cron:"+agentName, title, agentName, models.ChatThreadKindCron)
}

// GetThread returns a thread the user may read: owned by userID, or a
// registered channel thread in the same project (any project member).
func (s *PmService) GetThread(projectID, threadID, userID string) (models.ChatThread, error) {
	var t models.ChatThread
	if err := s.db.Where("id = ? AND project_id = ?", threadID, projectID).First(&t).Error; err != nil {
		return models.ChatThread{}, ErrPmThreadNotFound
	}
	if t.UserID == userID || IsChannelUserID(t.UserID) {
		return t, nil
	}
	return models.ChatThread{}, ErrPmThreadNotFound
}

// RequireWritableThread loads a readable thread and rejects channel threads
// for Web write/delete/turn paths.
func (s *PmService) RequireWritableThread(projectID, threadID, userID string) (models.ChatThread, error) {
	t, err := s.GetThread(projectID, threadID, userID)
	if err != nil {
		return models.ChatThread{}, err
	}
	if IsChannelUserID(t.UserID) {
		return models.ChatThread{}, ErrPmChannelReadOnly
	}
	return t, nil
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

// ListConversationsForAgent returns threads visible to a context-store session.
// Interactive users see their own user threads plus the agent's cron threads.
// Cron sessions (userID == "cron") only see cron threads for the agent.
func (s *PmService) ListConversationsForAgent(projectID, agentName, userID string) ([]models.ChatThread, error) {
	if _, ok := s.project(projectID); !ok {
		return nil, ErrProjectNotFound
	}
	q := s.db.Where("project_id = ? AND agent_name = ?", projectID, agentName)
	switch {
	case userID == "cron":
		q = q.Where("kind = ?", models.ChatThreadKindCron)
	case userID != "":
		q = q.Where(
			"(kind = ? OR ((kind = ? OR kind = '' OR kind IS NULL) AND user_id = ?))",
			models.ChatThreadKindCron, models.ChatThreadKindUser, userID,
		)
	}
	var threads []models.ChatThread
	if err := q.Order("updated_at desc").Find(&threads).Error; err != nil {
		return nil, err
	}
	return threads, nil
}

// DeleteThread removes a thread and its messages (owner only; channel threads rejected).
func (s *PmService) DeleteThread(projectID, threadID, userID string) error {
	if _, err := s.RequireWritableThread(projectID, threadID, userID); err != nil {
		return err
	}
	return s.DeleteThreadByID(threadID)
}

// DeleteThreadByID removes a thread and its messages without an ownership check
// (used for cron job cleanup / failed job create rollback).
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

// ListCronJobs returns all AgentCronJob rows for a project (any agent).
func (s *PmService) ListCronJobs(projectID string) ([]models.AgentCronJob, error) {
	if _, ok := s.project(projectID); !ok {
		return nil, ErrProjectNotFound
	}
	var jobs []models.AgentCronJob
	if err := s.db.Where("project_id = ?", projectID).Order("updated_at desc").Find(&jobs).Error; err != nil {
		return nil, err
	}
	if jobs == nil {
		jobs = []models.AgentCronJob{}
	}
	return jobs, nil
}

// PatchCronJobDeliver updates deliverToChannel for a job scoped to projectID.
func (s *PmService) PatchCronJobDeliver(projectID, jobID string, deliver bool) (models.AgentCronJob, error) {
	if _, ok := s.project(projectID); !ok {
		return models.AgentCronJob{}, ErrProjectNotFound
	}
	var job models.AgentCronJob
	if err := s.db.Where("id = ? AND project_id = ?", jobID, projectID).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.AgentCronJob{}, ErrPmCronJobNotFound
		}
		return models.AgentCronJob{}, err
	}
	job.DeliverToChannel = deliver
	job.UpdatedAt = time.Now().UTC()
	if err := s.db.Model(&job).Select("DeliverToChannel", "UpdatedAt").Updates(job).Error; err != nil {
		return models.AgentCronJob{}, err
	}
	return job, nil
}

// DeleteCronJob removes a project-scoped cron job with the same cleanup as
// DeleteCronJobForAgent (runs, job, and exclusive cron thread/messages/drafts).
func (s *PmService) DeleteCronJob(projectID, jobID string) error {
	if _, ok := s.project(projectID); !ok {
		return ErrProjectNotFound
	}
	var job models.AgentCronJob
	if err := s.db.Where("id = ? AND project_id = ?", jobID, projectID).First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPmCronJobNotFound
		}
		return err
	}
	return s.DeleteCronJobForAgent(projectID, job.AgentName, jobID)
}

// GetThreadForAgent returns one thread when it belongs to project+agent.
func (s *PmService) GetThreadForAgent(projectID, agentName, threadID string) (models.ChatThread, error) {
	agentName = strings.TrimSpace(agentName)
	var t models.ChatThread
	if err := s.db.Where("id = ? AND project_id = ? AND agent_name = ?", threadID, projectID, agentName).
		First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.ChatThread{}, ErrPmThreadNotFound
		}
		return models.ChatThread{}, err
	}
	return t, nil
}

// ListThreadsForAgent returns every thread (any owner/kind) for a project+agent.
// Used by Agent Studio context management (not the per-user consult sidebar).
func (s *PmService) ListThreadsForAgent(projectID, agentName string) ([]models.ChatThread, error) {
	if _, ok := s.project(projectID); !ok {
		return nil, ErrProjectNotFound
	}
	agentName = strings.TrimSpace(agentName)
	var threads []models.ChatThread
	if err := s.db.Where("project_id = ? AND agent_name = ?", projectID, agentName).
		Order("updated_at desc").Find(&threads).Error; err != nil {
		return nil, err
	}
	if threads == nil {
		threads = []models.ChatThread{}
	}
	return threads, nil
}

// DeleteThreadForAgent deletes a thread only when it belongs to project+agent.
func (s *PmService) DeleteThreadForAgent(projectID, agentName, threadID string) error {
	agentName = strings.TrimSpace(agentName)
	threadID = strings.TrimSpace(threadID)
	var t models.ChatThread
	if err := s.db.Where("id = ? AND project_id = ? AND agent_name = ?", threadID, projectID, agentName).
		First(&t).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPmThreadNotFound
		}
		return err
	}
	return s.DeleteThreadByID(threadID)
}

// ListCronJobsForAgent returns jobs scoped to one project+agent.
func (s *PmService) ListCronJobsForAgent(projectID, agentName string) ([]models.AgentCronJob, error) {
	if _, ok := s.project(projectID); !ok {
		return nil, ErrProjectNotFound
	}
	agentName = strings.TrimSpace(agentName)
	var jobs []models.AgentCronJob
	if err := s.db.Where("project_id = ? AND agent_name = ?", projectID, agentName).
		Order("updated_at desc").Find(&jobs).Error; err != nil {
		return nil, err
	}
	if jobs == nil {
		jobs = []models.AgentCronJob{}
	}
	return jobs, nil
}

// PatchCronJobForAgent toggles enabled and/or deliverToChannel for a job.
func (s *PmService) PatchCronJobForAgent(projectID, agentName, jobID string, enabled, deliver *bool) (models.AgentCronJob, error) {
	agentName = strings.TrimSpace(agentName)
	var job models.AgentCronJob
	if err := s.db.Where("id = ? AND project_id = ? AND agent_name = ?", jobID, projectID, agentName).
		First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.AgentCronJob{}, ErrPmCronJobNotFound
		}
		return models.AgentCronJob{}, err
	}
	updates := map[string]any{"updated_at": time.Now().UTC()}
	if enabled != nil {
		updates["enabled"] = *enabled
		job.Enabled = *enabled
	}
	if deliver != nil {
		updates["deliver_to_channel"] = *deliver
		job.DeliverToChannel = *deliver
	}
	if err := s.db.Model(&job).Updates(updates).Error; err != nil {
		return models.AgentCronJob{}, err
	}
	return job, nil
}

// DeleteCronJobForAgent removes a job, its runs, and exclusive cron thread.
func (s *PmService) DeleteCronJobForAgent(projectID, agentName, jobID string) error {
	agentName = strings.TrimSpace(agentName)
	var job models.AgentCronJob
	if err := s.db.Where("id = ? AND project_id = ? AND agent_name = ?", jobID, projectID, agentName).
		First(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPmCronJobNotFound
		}
		return err
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("job_id = ?", job.ID).Delete(&models.AgentCronRun{}).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", job.ID).Delete(&models.AgentCronJob{}).Error; err != nil {
			return err
		}
		if strings.TrimSpace(job.ThreadID) == "" {
			return nil
		}
		if err := tx.Where("thread_id = ?", job.ThreadID).Delete(&models.ChatTurnDraft{}).Error; err != nil {
			return err
		}
		if err := tx.Where("thread_id = ?", job.ThreadID).Delete(&models.ChatMessage{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", job.ThreadID).Delete(&models.ChatThread{}).Error
	})
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
