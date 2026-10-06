package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/models"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// ListMessages returns messages for a thread (oldest first).
func (s *PmService) ListMessages(threadID string) ([]models.ChatMessage, error) {
	var msgs []models.ChatMessage
	if err := s.db.Where("thread_id = ?", threadID).Order("created_at asc").Find(&msgs).Error; err != nil {
		return nil, err
	}
	return msgs, nil
}

// ListMessagesWindow returns a newest-tail or before-cursor page of messages
// (oldest→newest). Without beforeID it returns the most recent `limit` rows;
// with beforeID it returns up to `limit` rows strictly older than that message.
// hasMore is true when older messages remain beyond the returned window.
// limit is clamped to [1, 100] (default 20). Unknown beforeID yields ErrPmMessageNotFound.
func (s *PmService) ListMessagesWindow(threadID string, limit int, beforeID string) ([]models.ChatMessage, bool, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	q := s.db.Where("thread_id = ?", threadID)
	if beforeID != "" {
		anchor, err := s.GetMessage(threadID, beforeID)
		if err != nil {
			return nil, false, err
		}
		// Strictly older than the anchor (created_at, then id for same-timestamp ties).
		q = q.Where(
			"(created_at < ?) OR (created_at = ? AND id < ?)",
			anchor.CreatedAt, anchor.CreatedAt, anchor.ID,
		)
	}
	var newestFirst []models.ChatMessage
	if err := q.Order("created_at desc, id desc").Limit(limit + 1).Find(&newestFirst).Error; err != nil {
		return nil, false, err
	}
	hasMore := len(newestFirst) > limit
	if hasMore {
		newestFirst = newestFirst[:limit]
	}
	// Reverse to oldest→newest for chat UI consumption.
	for i, j := 0, len(newestFirst)-1; i < j; i, j = i+1, j-1 {
		newestFirst[i], newestFirst[j] = newestFirst[j], newestFirst[i]
	}
	return newestFirst, hasMore, nil
}

// AppendMessage persists one chat message and bumps thread updated_at.
// AppendMessageSource additionally tags the turn origin (user | cron).
func (s *PmService) AppendMessage(threadID, role, content string, citations []models.ProgressCitation, attached *models.AttachedContext, images []models.PromptImage) (models.ChatMessage, error) {
	return s.AppendMessageSource(threadID, role, content, "", citations, attached, images, nil, nil)
}

// AppendMessageSource is AppendMessage with an explicit source tag and optional
// Usage / UsageByModel (assistant turns only; nil = not reported / not billed).
func (s *PmService) AppendMessageSource(threadID, role, content, source string, citations []models.ProgressCitation, attached *models.AttachedContext, images []models.PromptImage, usage *models.TokenUsage, usageByModel models.TokenUsageByModel) (models.ChatMessage, error) {
	if role == "" {
		return models.ChatMessage{}, fmt.Errorf("role required")
	}
	var err error
	images, err = blob.IngestPromptImages(context.Background(), s.blobs, images)
	if err != nil {
		return models.ChatMessage{}, fmt.Errorf("ingest attachments: %w", err)
	}
	msg := models.ChatMessage{
		ID: "msg-" + uuid.NewString()[:12], ThreadID: threadID, Role: role, Content: content,
		Status: "ok", Source: source, Images: images, Citations: citations, AttachedContext: attached,
		Usage: models.CloneTokenUsage(usage), UsageByModel: models.CloneTokenUsageByModel(usageByModel),
		CreatedAt: time.Now(),
	}
	if err := s.db.Create(&msg).Error; err != nil {
		return models.ChatMessage{}, err
	}
	if err := s.db.Model(&models.ChatThread{}).Where("id = ?", threadID).
		Updates(map[string]any{"updated_at": time.Now()}).Error; err != nil {
		log.Warn().Err(err).Str("thread", threadID).Msg("bump thread updated_at failed")
	}
	// Auto-title from first user message.
	if role == "user" {
		var t models.ChatThread
		if err := s.db.First(&t, "id = ?", threadID).Error; err == nil && (t.Title == "" || t.Title == "新会话") {
			title := strings.TrimSpace(content)
			if title == "" && len(images) > 0 {
				title = "图片消息"
			}
			if len([]rune(title)) > 40 {
				title = string([]rune(title)[:40]) + "…"
			}
			if title != "" {
				if err := s.db.Model(&t).Update("title", title).Error; err != nil {
					log.Warn().Err(err).Str("thread", threadID).Msg("auto-title thread failed")
				}
			}
		}
	}
	return msg, nil
}

// GetMessage loads one message by id within a thread.
func (s *PmService) GetMessage(threadID, messageID string) (models.ChatMessage, error) {
	var m models.ChatMessage
	if err := s.db.Where("id = ? AND thread_id = ?", messageID, threadID).First(&m).Error; err != nil {
		return models.ChatMessage{}, ErrPmMessageNotFound
	}
	return m, nil
}

// UpdateMessageFailure marks or clears failure metadata on a user message.
// status "failed" requires a valid failKind; status "ok" clears failKind.
// Only role=user messages may be updated (assistant/system are rejected).
func (s *PmService) UpdateMessageFailure(threadID, messageID, status, failKind string) (models.ChatMessage, error) {
	msg, err := s.GetMessage(threadID, messageID)
	if err != nil {
		return models.ChatMessage{}, err
	}
	if msg.Role != "user" {
		return models.ChatMessage{}, ErrPmMessageInvalidRole
	}
	switch status {
	case "ok", "":
		status = "ok"
		failKind = ""
	case "failed":
		if !validPmFailKind(failKind) {
			return models.ChatMessage{}, ErrPmMessageInvalidStatus
		}
	default:
		return models.ChatMessage{}, ErrPmMessageInvalidStatus
	}
	if err := s.db.Model(&models.ChatMessage{}).Where("id = ? AND thread_id = ?", messageID, threadID).
		Updates(map[string]any{"status": status, "fail_kind": failKind}).Error; err != nil {
		return models.ChatMessage{}, err
	}
	msg.Status = status
	msg.FailKind = failKind
	if err := s.db.Model(&models.ChatThread{}).Where("id = ?", threadID).
		Updates(map[string]any{"updated_at": time.Now()}).Error; err != nil {
		log.Warn().Err(err).Str("thread", threadID).Msg("bump thread updated_at failed")
	}
	return msg, nil
}

// RecentMessages returns the last n non-failed messages for context injection.
// Failed turns (and any failure placeholders) are skipped so retries do not pollute the agent preamble.
func (s *PmService) RecentMessages(threadID string, n int) ([]models.ChatMessage, error) {
	if n <= 0 {
		n = 20
	}
	var msgs []models.ChatMessage
	// Over-fetch then filter so we still return up to n usable turns after skipping failures.
	if err := s.db.Where("thread_id = ?", threadID).Order("created_at desc").Limit(n * 3).Find(&msgs).Error; err != nil {
		return nil, err
	}
	filtered := make([]models.ChatMessage, 0, n)
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Status == "failed" {
			continue
		}
		filtered = append(filtered, m)
	}
	if len(filtered) > n {
		filtered = filtered[len(filtered)-n:]
	}
	return filtered, nil
}

// CountMessagesByThreads returns message counts keyed by thread id.
func (s *PmService) CountMessagesByThreads(threadIDs []string) (map[string]int64, error) {
	out := map[string]int64{}
	if len(threadIDs) == 0 {
		return out, nil
	}
	type row struct {
		ThreadID string
		N        int64
	}
	var rows []row
	if err := s.db.Model(&models.ChatMessage{}).
		Select("thread_id as thread_id, count(*) as n").
		Where("thread_id IN ?", threadIDs).
		Group("thread_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ThreadID] = r.N
	}
	return out, nil
}

// GetMessagesPage returns messages for a thread with limit/offset (oldest-first page).
func (s *PmService) GetMessagesPage(threadID string, limit, offset int) ([]models.ChatMessage, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	var total int64
	if err := s.db.Model(&models.ChatMessage{}).Where("thread_id = ?", threadID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var msgs []models.ChatMessage
	if err := s.db.Where("thread_id = ?", threadID).Order("created_at asc").
		Offset(offset).Limit(limit).Find(&msgs).Error; err != nil {
		return nil, 0, err
	}
	return msgs, total, nil
}

// SearchMessages finds messages visible to a context-store session by keyword.
func (s *PmService) SearchMessages(projectID, agentName, userID, q string, limit int) ([]map[string]any, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("query required")
	}
	if limit <= 0 {
		limit = 20
	}
	threads, err := s.ListConversationsForAgent(projectID, agentName, userID)
	if err != nil {
		return nil, err
	}
	if len(threads) == 0 {
		return []map[string]any{}, nil
	}
	ids := make([]string, len(threads))
	titleBy := map[string]string{}
	for i, t := range threads {
		ids[i] = t.ID
		titleBy[t.ID] = t.Title
	}
	var msgs []models.ChatMessage
	like := "%" + EscapeLike(q) + "%"
	if err := s.db.Where("thread_id IN ? AND content LIKE ? ESCAPE ?", ids, like, `\`).
		Order("created_at desc").Limit(limit).Find(&msgs).Error; err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		snippet := m.Content
		if len([]rune(snippet)) > 160 {
			snippet = string([]rune(snippet)[:160]) + "…"
		}
		out = append(out, map[string]any{
			"messageId": m.ID, "conversationId": m.ThreadID,
			"title": titleBy[m.ThreadID], "role": m.Role, "snippet": snippet,
			"createdAt": m.CreatedAt,
		})
	}
	return out, nil
}
