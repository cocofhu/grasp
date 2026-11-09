package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
)

type SoloMessagePage struct {
	Items   []models.SoloMessage `json:"items"`
	Before  string               `json:"before,omitempty"`
	HasMore bool                 `json:"hasMore"`
}

type soloMessageCursor struct {
	Session string    `json:"session"`
	ID      string    `json:"id"`
	At      time.Time `json:"at"`
}

// MessagePage reads a tail or an older window without loading process bodies.
// The immutable (created_at,id) anchor is stable when new messages arrive.
func (s *SoloService) MessagePage(ctx context.Context, id, user, before string, limit int) (SoloMessagePage, error) {
	result := SoloMessagePage{Items: []models.SoloMessage{}}
	if _, err := s.Require(id, user); err != nil {
		return result, err
	}
	if limit < 1 || limit > 200 {
		return result, soloErr(400, "invalid_page", "消息页大小必须为 1 到 200")
	}
	query := s.db.WithContext(ctx).Where("session_id = ?", id).Omit("events", "workflow_context")
	if before != "" {
		var cursor soloMessageCursor
		body, err := base64.RawURLEncoding.DecodeString(before)
		if err != nil || len(body) > 1024 || json.Unmarshal(body, &cursor) != nil || cursor.Session != id || cursor.ID == "" || cursor.At.IsZero() {
			return result, soloErr(400, "invalid_cursor", "消息分页游标无效，请重新加载")
		}
		var count int64
		if err := s.db.WithContext(ctx).Model(&models.SoloMessage{}).Where("session_id = ? AND id = ? AND created_at = ?", id, cursor.ID, cursor.At).Count(&count).Error; err != nil {
			return result, err
		}
		if count != 1 {
			return result, soloErr(400, "invalid_cursor", "消息分页游标已失效，请重新加载")
		}
		query = query.Where("created_at < ? OR (created_at = ? AND id < ?)", cursor.At, cursor.At, cursor.ID)
	}
	var rows []models.SoloMessage
	if err := query.Order("created_at DESC, id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return result, err
	}
	result.HasMore = len(rows) > limit
	if result.HasMore {
		rows = rows[:limit]
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Role == "assistant" && row.EventLog == nil {
			ids = append(ids, row.ID)
		}
	}
	if len(ids) > 0 {
		var legacyIDs []string
		if err := s.db.WithContext(ctx).Model(&models.SoloMessage{}).Where("session_id = ? AND id IN ? AND events IS NOT NULL AND events NOT IN ?", id, ids, []string{"", "null", "[]"}).Pluck("id", &legacyIDs).Error; err != nil {
			return result, err
		}
		legacy := map[string]bool{}
		for _, mid := range legacyIDs {
			legacy[mid] = true
		}
		for i := range rows {
			rows[i].LegacyEvents = legacy[rows[i].ID]
		}
	}
	for i := len(rows) - 1; i >= 0; i-- {
		result.Items = append(result.Items, rows[i])
	}
	if result.HasMore {
		oldest := result.Items[0]
		body, err := json.Marshal(soloMessageCursor{Session: id, ID: oldest.ID, At: oldest.CreatedAt})
		if err != nil {
			return result, err
		}
		result.Before = base64.RawURLEncoding.EncodeToString(body)
	}
	return result, nil
}

type SoloMessageEventPage struct {
	chatsession.JournalPage
	LegacyIncomplete bool `json:"legacyIncomplete,omitempty"`
}

// MessageEvents authorizes the specific message before looking up any body.
// New history is read in bounded journal pages; old inline history remains a
// migration reader and honestly marks the former 6000-event truncation bound.
func (s *SoloService) MessageEvents(ctx context.Context, id, user, messageID string, after int64) (SoloMessageEventPage, error) {
	result := SoloMessageEventPage{JournalPage: chatsession.JournalPage{Items: []chatsession.JournalEntry{}, Next: after, Head: -1}}
	if _, err := s.Require(id, user); err != nil {
		return result, err
	}
	var message models.SoloMessage
	if err := s.db.WithContext(ctx).Omit("events", "content", "images", "annotations", "workflow_context").First(&message, "id = ? AND session_id = ? AND role = ?", messageID, id, "assistant").Error; err != nil {
		return result, err
	}
	if message.EventLog != nil {
		pointer := message.EventLog
		if pointer.Version != chatsession.JournalVersion || pointer.From < 0 || pointer.Through < pointer.From || pointer.Count < 1 {
			return result, chatsession.ErrJournalGap
		}
		actual, err := s.journalPointer(s.db.WithContext(ctx), id, message.TurnID, pointer.Generation)
		if err != nil {
			return result, err
		}
		if actual == nil || *actual != *pointer {
			return result, chatsession.ErrJournalGap
		}
		if after == -1 {
			after = pointer.From - 1
		}
		if after < pointer.From-1 || after > pointer.Through {
			return result, chatsession.ErrJournalCursor
		}
		page, err := s.durable.JournalPage(ctx, soloQueueScope(id), chatsession.JournalQuery{After: after, Through: &pointer.Through})
		if err != nil {
			return result, err
		}
		result.JournalPage = page
		result.Items = []chatsession.JournalEntry{}
		for _, entry := range page.Items {
			if entry.Type == chatsession.FrameAcp {
				if entry.TurnID != message.TurnID || entry.Generation != pointer.Generation {
					return SoloMessageEventPage{}, chatsession.ErrJournalGap
				}
				result.Items = append(result.Items, entry)
			}
		}
		return result, nil
	}
	if err := s.db.WithContext(ctx).Select("events").First(&message, "id = ? AND session_id = ?", messageID, id).Error; err != nil {
		return result, err
	}
	result.Head = int64(len(message.Events)) - 1
	if after < -1 || after > result.Head {
		return result, chatsession.ErrJournalCursor
	}
	through := after + 256
	if through > result.Head {
		through = result.Head
	}
	bytes := 0
	for seq := after + 1; seq <= through; seq++ {
		data := message.Events[seq]
		if len(result.Items) > 0 && bytes+len(data) > chatsession.JournalPageBytes {
			break
		}
		result.Items = append(result.Items, chatsession.JournalEntry{Seq: uint64(seq), Type: chatsession.FrameAcp, TurnID: message.TurnID, At: message.CreatedAt, Data: data})
		result.Next, bytes = seq, bytes+len(data)
	}
	result.HasMore = result.Next < result.Head
	result.LegacyIncomplete = len(message.Events) >= 6000
	return result, nil
}
