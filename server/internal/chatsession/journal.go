package chatsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
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
	ErrJournalGap    = errors.New("conversation event journal is incomplete")
	ErrJournalCursor = errors.New("conversation event cursor is invalid")
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

// AppendJournal commits the body and its index before the caller may publish
// any included frame. ACP frames always require the executing owner. Control
// frames can use validate to compose their owning session's access/lifecycle
// check under the same queue lock, including after native execution finishes.
func (s *DurableStore) AppendJournal(ctx context.Context, scope QueueScope, owner *QueueLease, turnID string, entries []JournalEntry, validate func(*gorm.DB) error) (JournalRange, error) {
	var result JournalRange
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = s.AppendJournalTx(ctx, tx, scope, owner, turnID, entries, validate)
		return err
	})
	if err != nil {
		return JournalRange{}, err
	}
	return result, nil
}

// AppendJournalTx composes event publication with the caller's existing
// lifecycle/permission transaction. The caller must commit before publishing.
func (s *DurableStore) AppendJournalTx(ctx context.Context, tx *gorm.DB, scope QueueScope, owner *QueueLease, turnID string, entries []JournalEntry, validate func(*gorm.DB) error) (JournalRange, error) {
	var result JournalRange
	if len(entries) == 0 || len(entries) > JournalBatchEvents || len(turnID) > 191 {
		return result, errors.New("invalid conversation event batch")
	}
	for _, entry := range entries {
		if entry.Type != FrameSession && entry.Type != FrameAcp || entry.Type == FrameSession && (entry.Event == "" || len(entry.Event) > 64) || entry.Type == FrameAcp && (!json.Valid(entry.Data) || owner == nil) {
			return result, errors.New("invalid conversation event")
		}
	}
	queue, err := s.lockJournalTx(ctx, tx, scope, owner, turnID, validate)
	if err != nil {
		return JournalRange{}, err
	}
	if queue.NextEventSequence > journalSafeSequence-uint64(len(entries)) {
		return JournalRange{}, errors.New("conversation event sequence exhausted")
	}
	persisted := make([]JournalEntry, len(entries))
	for i, entry := range entries {
		if entry.At.IsZero() {
			entry.At = s.now()
		}
		entry.Seq, entry.Generation, entry.TurnID = queue.NextEventSequence+uint64(i), queue.Generation, turnID
		entry.Data, entry.Payload = append(json.RawMessage(nil), entry.Data...), maps.Clone(entry.Payload)
		persisted[i] = entry
	}
	result = JournalRange{First: queue.NextEventSequence, Last: queue.NextEventSequence + uint64(len(entries)) - 1, Count: len(entries)}
	body, err := json.Marshal(journalEnvelope{JournalVersion, queue.ID, result.First, result.Last, persisted})
	if err != nil {
		return JournalRange{}, err
	}
	if len(body) > JournalChunkBytes {
		return JournalRange{}, errors.New("conversation event batch exceeds storage bound")
	}
	chunk := models.ChatEventChunk{ID: uuid.NewString(), QueueID: queue.ID, FirstSequence: result.First,
		LastSequence: result.Last, TurnID: turnID, ExecutionEpoch: queue.Generation, Count: len(entries), Bytes: len(body), Payload: body}
	for _, entry := range entries {
		if entry.Type == FrameAcp {
			chunk.AcpCount++
		}
	}
	if len(entries) == 1 && entries[0].Type == FrameSession {
		chunk.ControlEvent = entries[0].Event
	}
	if err := tx.Create(&chunk).Error; err != nil {
		return JournalRange{}, err
	}
	if err := tx.Model(queue).Update("next_event_sequence", result.Last+1).Error; err != nil {
		return JournalRange{}, err
	}
	return result, nil
}

// FenceJournalTx acquires the conversation lock before a caller takes a lower
// level sandbox lock. It performs SQL/ownership validation without body I/O;
// AppendJournalTx may safely re-enter it in the same transaction.
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

// JournalPage reads one bounded ordered window. Missing/corrupt chunks fail the
// entire window without advancing its cursor. A single large frame may exceed
// the normal page target, but remains bounded by JournalChunkBytes.
func (s *DurableStore) JournalPage(ctx context.Context, scope QueueScope, query JournalQuery) (JournalPage, error) {
	result := JournalPage{Items: []JournalEntry{}, Next: query.After, Head: -1}
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
		bytes := 0
		for result.Next < result.Head && len(result.Items) < limit {
			var row models.ChatEventChunk
			err := tx.Where("queue_id = ? AND last_sequence >= ?", queue.ID, uint64(result.Next+1)).Order("last_sequence").First(&row).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrJournalGap
			}
			if err != nil {
				return err
			}
			entries, err := validateJournalChunk(row)
			if err != nil {
				return err
			}
			if int64(row.FirstSequence) > result.Next+1 {
				return ErrJournalGap
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
					return err
				}
				if len(result.Items) > 0 && bytes+len(encoded) > JournalPageBytes {
					result.HasMore = true
					return nil
				}
				result.Items = append(result.Items, entry)
				result.Next = int64(entry.Seq)
				bytes += len(encoded)
				if len(result.Items) == limit {
					break
				}
			}
		}
		result.HasMore = result.Next < result.Head
		return nil
	})
	if err != nil {
		return JournalPage{Items: []JournalEntry{}, Next: query.After, Head: -1}, err
	}
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
