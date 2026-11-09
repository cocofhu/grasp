package chatsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	JournalVersion     = 1
	JournalBatchEvents = 128
	// A large individual ACP event is retained as one chunk, up to this
	// explicit bound. A refusal is an error, never a successful partial save.
	JournalChunkBytes   = 16 << 20
	JournalPageBytes    = 1 << 20
	JournalPageEvents   = 512
	journalSafeSequence = 1<<53 - 1
)

var (
	ErrJournalGap             = errors.New("conversation event journal is incomplete")
	ErrJournalCursor          = errors.New("conversation event cursor is invalid")
	ErrJournalPrepareConflict = errors.New("conversation event head changed after body preparation")
)

type JournalEntry struct {
	Seq        uint64          `json:"seq"`
	Type       string          `json:"type"`
	Event      string          `json:"event,omitempty"`
	TurnID     string          `json:"turnId,omitempty"`
	Generation uint64          `json:"generation"`
	At         time.Time       `json:"at"`
	Data       json.RawMessage `json:"data,omitempty"`
	Payload    map[string]any  `json:"payload,omitempty"`
}

func (e JournalEntry) Frame() map[string]any {
	out := maps.Clone(e.Payload)
	if out == nil {
		out = map[string]any{}
	}
	out["seq"], out["type"] = e.Seq, e.Type
	if e.Type == FrameAcp {
		out["data"] = e.Data
	} else {
		out["event"] = e.Event
	}
	if e.TurnID != "" {
		out["userMsgId"] = e.TurnID
	}
	out["sessionGeneration"] = e.Generation
	out["at"] = e.At
	return out
}

func (e JournalEntry) StreamEvent() Event {
	payload := maps.Clone(e.Payload)
	for _, key := range []string{"seq", "type", "event", "userMsgId", "sessionGeneration", "at"} {
		delete(payload, key)
	}
	return Event{Seq: int(e.Seq), Type: e.Type, Event: e.Event, Data: append(json.RawMessage(nil), e.Data...), Payload: payload, TurnID: e.TurnID, Generation: e.Generation, At: e.At}
}

type journalEnvelope struct {
	Version int            `json:"version"`
	QueueID string         `json:"queueId"`
	First   uint64         `json:"first"`
	Last    uint64         `json:"last"`
	Entries []JournalEntry `json:"entries"`
}

type JournalRange struct {
	First uint64
	Last  uint64
	Count int
}

// FenceJournalTx acquires the conversation lock before a caller takes a lower
// level sandbox lock. It performs SQL/ownership validation without body I/O;
// AppendPreparedJournalTx may safely re-enter it in the same transaction.
func (s *DurableStore) FenceJournalTx(ctx context.Context, tx *gorm.DB, scope QueueScope, owner *QueueLease, turnID string, validate func(*gorm.DB) error) error {
	_, err := s.lockJournalTx(ctx, tx, scope, owner, turnID, validate)
	return err
}

func (s *DurableStore) lockJournalTx(ctx context.Context, tx *gorm.DB, scope QueueScope, owner *QueueLease, turnID string, validate func(*gorm.DB) error) (*models.ChatQueue, error) {
	queue, err := s.lock(tx, scope)
	if err != nil {
		return nil, err
	}
	if owner != nil {
		if owner.Scope != scope {
			return nil, ErrQueueOwnerLost
		}
		if execution, ok := CurrentExecution(ctx); ok {
			if execution.Lease.Scope != owner.Scope || execution.Lease.Owner != owner.Owner || execution.Lease.Generation != owner.Generation || execution.TurnID != turnID {
				return nil, ErrQueueOwnerLost
			}
			if err := execution.checkOwner(); err != nil {
				return nil, err
			}
		}
		if err := s.Fence(tx, *owner, turnID); err != nil {
			return nil, err
		}
	}
	if validate != nil {
		if err := validate(tx); err != nil {
			return nil, err
		}
	}
	return queue, nil
}

type JournalQuery struct {
	After   int64
	Through *int64
	Limit   int
}

type JournalPage struct {
	Items   []JournalEntry `json:"items"`
	Next    int64          `json:"next"`
	Head    int64          `json:"head"`
	HasMore bool           `json:"hasMore"`
}

func validateJournalChunk(row models.ChatEventChunk) ([]JournalEntry, error) {
	if row.Count < 1 || row.Count > JournalBatchEvents || row.Bytes != len(row.Payload) || row.Bytes > JournalChunkBytes || row.FirstSequence > journalSafeSequence || row.LastSequence > journalSafeSequence || row.LastSequence < row.FirstSequence || row.FirstSequence+uint64(row.Count)-1 != row.LastSequence {
		return nil, ErrJournalGap
	}
	var envelope journalEnvelope
	if err := json.Unmarshal(row.Payload, &envelope); err != nil {
		return nil, fmt.Errorf("%w: unreadable chunk", ErrJournalGap)
	}
	if envelope.Version != JournalVersion || envelope.QueueID != row.QueueID || envelope.First != row.FirstSequence || envelope.Last != row.LastSequence || len(envelope.Entries) != row.Count {
		return nil, ErrJournalGap
	}
	for i, entry := range envelope.Entries {
		if entry.Seq != row.FirstSequence+uint64(i) || entry.TurnID != row.TurnID || entry.Generation != row.ExecutionEpoch || entry.Type != FrameSession && entry.Type != FrameAcp || entry.Type == FrameSession && (entry.Event == "" || len(entry.Event) > 64) || entry.Type == FrameAcp && !json.Valid(entry.Data) {
			return nil, ErrJournalGap
		}
	}
	return envelope.Entries, nil
}

// DecodeJournalChunk validates an already loaded immutable body without SQL
// or object storage I/O. Readers may capture indexes under a short transaction,
// then release that transaction before loading and decoding the body.
func DecodeJournalChunk(row models.ChatEventChunk) ([]JournalEntry, error) {
	return validateJournalChunk(row)
}

// LoadJournalChunk resolves a raw record captured by an authorized SQL
// snapshot. The caller must release that snapshot before calling this method.
func (s *DurableStore) LoadJournalChunk(ctx context.Context, row JournalChunkRecord) ([]JournalEntry, error) {
	if err := s.outsidePayloadTransaction(); err != nil {
		return nil, err
	}
	if row.Count < 1 || row.Count > JournalBatchEvents || row.Bytes < 1 || row.Bytes > JournalChunkBytes ||
		row.AcpCount < 0 || row.AcpCount > row.Count || len(row.Payload) > JournalChunkBytes ||
		row.LastSequence > journalSafeSequence || row.FirstSequence > row.LastSequence || row.FirstSequence+uint64(row.Count)-1 != row.LastSequence {
		return nil, ErrJournalGap
	}
	files, err := filestore.FromDB(s.db)
	if err != nil {
		return nil, err
	}
	body, err := files.LoadBounded(ctx, row.Payload, filestore.Bytes, JournalChunkBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJournalGap, err)
	}
	return validateJournalChunk(row.logical(body))
}

// JournalPage reads one bounded ordered window. Missing/corrupt chunks fail the
// entire window without advancing its cursor. A single large frame may exceed
// the normal page target, but remains bounded by JournalChunkBytes.
func (s *DurableStore) JournalPage(ctx context.Context, scope QueueScope, query JournalQuery) (JournalPage, error) {
	result := JournalPage{Items: []JournalEntry{}, Next: query.After, Head: -1}
	if err := s.outsidePayloadTransaction(); err != nil {
		return result, err
	}
	if query.After < -1 || query.After > journalSafeSequence || query.Through != nil && (*query.Through < query.After || *query.Through > journalSafeSequence) {
		return result, ErrJournalCursor
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 256
	}
	if limit > JournalPageEvents {
		limit = JournalPageEvents
	}
	// Capture a bounded immutable index/ref window under the SQL snapshot.
	// Never query the serializer-bearing legacy chunk model here: even a GET
	// after its own First would still happen inside this outer transaction.
	var chunks []JournalChunkRecord
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var queue models.ChatQueue
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&queue, "id = ?", scope.ID()).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				if query.After == -1 {
					return nil
				}
				return ErrJournalCursor
			}
			return err
		}
		if queue.NextEventSequence > journalSafeSequence {
			return ErrJournalGap
		}
		result.Head = int64(queue.NextEventSequence) - 1
		if query.Through != nil && result.Head > *query.Through {
			result.Head = *query.Through
		}
		if query.After > result.Head {
			return ErrJournalCursor
		}
		cursor, count, bytes := query.After, 0, 0
		for cursor < result.Head && count < limit && bytes < JournalPageBytes {
			var row JournalChunkRecord
			err := tx.Table("chat_event_chunks").Where("queue_id = ? AND last_sequence >= ? AND first_sequence <= ?", queue.ID, uint64(cursor+1), uint64(result.Head)).Order("last_sequence").Take(&row).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrJournalGap
			}
			if err != nil {
				return err
			}
			if row.Count < 1 || row.Count > JournalBatchEvents || row.Bytes < 1 || row.Bytes > JournalChunkBytes ||
				row.AcpCount < 0 || row.AcpCount > row.Count || len(row.Payload) > JournalChunkBytes ||
				row.LastSequence > journalSafeSequence || row.FirstSequence > row.LastSequence ||
				row.FirstSequence+uint64(row.Count)-1 != row.LastSequence || int64(row.FirstSequence) > cursor+1 {
				return ErrJournalGap
			}
			chunks = append(chunks, row)
			through := min(int64(row.LastSequence), result.Head)
			count += int(through - cursor)
			cursor = through
			bytes += row.Bytes
		}
		return nil
	})
	if err != nil {
		return JournalPage{Items: []JournalEntry{}, Next: query.After, Head: -1}, err
	}
	if len(chunks) > 0 {
		bytes := 0
		for _, row := range chunks {
			entries, err := s.LoadJournalChunk(ctx, row)
			if err != nil {
				return JournalPage{Items: []JournalEntry{}, Next: query.After, Head: -1}, err
			}
			for _, entry := range entries {
				if int64(entry.Seq) <= result.Next {
					continue
				}
				if int64(entry.Seq) > result.Head {
					break
				}
				encoded, err := json.Marshal(entry)
				if err != nil {
					return JournalPage{Items: []JournalEntry{}, Next: query.After, Head: -1}, err
				}
				if len(result.Items) > 0 && bytes+len(encoded) > JournalPageBytes {
					result.HasMore = true
					return result, nil
				}
				result.Items = append(result.Items, entry)
				result.Next = int64(entry.Seq)
				bytes += len(encoded)
				if len(result.Items) == limit {
					result.HasMore = result.Next < result.Head
					return result, nil
				}
			}
		}
	}
	result.HasMore = result.Next < result.Head
	return result, nil
}

func (s *DurableStore) JournalHead(ctx context.Context, scope QueueScope) (int64, error) {
	var queue models.ChatQueue
	err := s.db.WithContext(ctx).Select("next_event_sequence").First(&queue, "id = ?", scope.ID()).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return -1, nil
	}
	if queue.NextEventSequence > journalSafeSequence {
		return -1, ErrJournalGap
	}
	return int64(queue.NextEventSequence) - 1, err
}
