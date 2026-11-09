package services

import (
	"encoding/json"
	"errors"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/historymigration"
	"github.com/cocofhu/grasp/internal/models"
)

var errConversationHistoryBound = errors.New("conversation history tool results exceed the request limit (100000 events or 256 MiB); request fewer messages")

type conversationJournalBudget struct {
	events int
	bytes  int
}

func (b *conversationJournalBudget) add(events, bytes int) error {
	if events < 0 || bytes < 0 || events > historymigration.MaxSourceEvents-b.events || bytes > historymigration.MaxSourceBytes-b.bytes {
		return errConversationHistoryBound
	}
	b.events += events
	b.bytes += bytes
	return nil
}

// hydrateJournalTools runs only after the caller has authorized the conversation
// and trimmed its message page. Modern messages keep their immutable ACP bodies
// in the journal, rather than the old Events cell. One budget covers the whole
// tool-event result, including already loaded legacy events and controls.
func (s *ConversationHistoryService) hydrateJournalTools(scope historyScope, rows []models.SoloMessage) error {
	var budget conversationJournalBudget
	for _, row := range rows {
		pointer := row.EventLog
		if pointer == nil || pointer.Legacy != nil {
			if err := budget.add(len(row.Events), 0); err != nil {
				return err
			}
			for _, raw := range row.Events {
				if err := budget.add(0, len(raw)); err != nil {
					return err
				}
			}
			continue
		}
		if row.Role != "assistant" || row.SessionID != scope.SessionID || row.TurnID == "" || pointer.Version != chatsession.JournalVersion ||
			pointer.Generation == 0 || pointer.From < 0 || pointer.Through < pointer.From || pointer.Count < 1 {
			return chatsession.ErrJournalGap
		}
		// Reserve the entire fixed range before object reads, including controls.
		// Subtract before adding one so an untrusted Through cannot overflow.
		if pointer.Through-pointer.From >= int64(historymigration.MaxSourceEvents) {
			return errConversationHistoryBound
		}
		if err := budget.add(int(pointer.Through-pointer.From)+1, 0); err != nil {
			return err
		}
	}
	ctx := s.db.Statement.Context
	reader := chatsession.NewDurableStore(s.db)
	for i := range rows {
		row := &rows[i]
		pointer := row.EventLog
		if pointer == nil || pointer.Legacy != nil {
			continue
		}
		// Match only the captured range. A current turn may append later chunks
		// while an agent reads history; those never expand this message's pointer.
		var indexed struct {
			FirstSequence int64
			LastSequence  int64
			AcpCount      int64
		}
		if err := s.db.WithContext(ctx).Table("chat_event_chunks").
			Select("COALESCE(MIN(first_sequence), 0) AS first_sequence, COALESCE(MAX(last_sequence), 0) AS last_sequence, COALESCE(SUM(acp_count), 0) AS acp_count").
			Where("queue_id = ? AND turn_id = ? AND execution_epoch = ? AND acp_count > 0 AND last_sequence >= ? AND first_sequence <= ?",
				soloQueueScope(scope.SessionID).ID(), row.TurnID, pointer.Generation, pointer.From, pointer.Through).Scan(&indexed).Error; err != nil {
			return err
		}
		if indexed.FirstSequence != pointer.From || indexed.LastSequence != pointer.Through || indexed.AcpCount != int64(pointer.Count) {
			return chatsession.ErrJournalGap
		}
		var events []json.RawMessage
		cursor := pointer.From - 1
		for cursor < pointer.Through {
			page, err := reader.JournalPage(ctx, soloQueueScope(scope.SessionID), chatsession.JournalQuery{After: cursor, Through: &pointer.Through})
			if err != nil {
				return err
			}
			if page.Head != pointer.Through || page.Next <= cursor || page.Next > pointer.Through {
				return chatsession.ErrJournalGap
			}
			for _, entry := range page.Items {
				if entry.Seq != uint64(cursor+1) {
					return chatsession.ErrJournalGap
				}
				body, err := json.Marshal(entry)
				if err != nil {
					return err
				}
				if err := budget.add(0, len(body)); err != nil {
					return err
				}
				cursor = int64(entry.Seq)
				if entry.Type != chatsession.FrameAcp {
					continue
				}
				if entry.TurnID != row.TurnID || entry.Generation != pointer.Generation || len(events) >= pointer.Count {
					return chatsession.ErrJournalGap
				}
				events = append(events, entry.Data)
			}
			if cursor != page.Next || page.HasMore != (cursor < pointer.Through) {
				return chatsession.ErrJournalGap
			}
		}
		if len(events) != pointer.Count {
			return chatsession.ErrJournalGap
		}
		row.Events = events
	}
	return nil
}
