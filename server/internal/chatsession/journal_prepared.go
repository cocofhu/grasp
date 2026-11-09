package chatsession

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const journalCommitTimeout = 10 * time.Second

// JournalChunkRecord maps the existing cells without a GORM serializer. The
// legacy model remains readable by old callers and the backup catalogue; all
// journal storage paths explicitly publish/load its payload outside SQL.
type JournalChunkRecord struct {
	ID             string
	QueueID        string
	FirstSequence  uint64
	LastSequence   uint64
	TurnID         string
	ExecutionEpoch uint64
	Count          int
	AcpCount       int
	ControlEvent   string
	Bytes          int
	Payload        string `json:"-"`
	CreatedAt      time.Time
}

func (row JournalChunkRecord) logical(body []byte) models.ChatEventChunk {
	return models.ChatEventChunk{ID: row.ID, QueueID: row.QueueID, FirstSequence: row.FirstSequence,
		LastSequence: row.LastSequence, TurnID: row.TurnID, ExecutionEpoch: row.ExecutionEpoch,
		Count: row.Count, AcpCount: row.AcpCount, ControlEvent: row.ControlEvent, Bytes: row.Bytes,
		Payload: body, CreatedAt: row.CreatedAt}
}

type JournalBase struct {
	scope      QueueScope
	generation uint64
	next       uint64
}

// ReadJournalBase captures metadata and releases its cursor before encoding or
// object publication. A missing queue is initialized in a short SQL-only TX;
// this never reserves an event sequence or exposes an unfilled journal range.
func (s *DurableStore) ReadJournalBase(ctx context.Context, scope QueueScope) (JournalBase, error) {
	if err := s.outsidePayloadTransaction(); err != nil {
		return JournalBase{}, err
	}
	if err := scope.validate(); err != nil {
		return JournalBase{}, err
	}
	var queue models.ChatQueue
	err := s.db.WithContext(ctx).Select("id", "generation", "next_event_sequence").First(&queue, "id = ?", scope.ID()).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && queue.Generation == 0 {
		err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			locked, lockErr := s.lock(tx, scope)
			if lockErr == nil {
				queue = *locked
			}
			return lockErr
		})
	}
	if err != nil {
		return JournalBase{}, err
	}
	if queue.NextEventSequence > journalSafeSequence {
		return JournalBase{}, ErrJournalGap
	}
	return JournalBase{scope: scope, generation: queue.Generation, next: queue.NextEventSequence}, nil
}

type journalBatch struct {
	scope   QueueScope
	owner   *QueueLease
	turnID  string
	entries []JournalEntry
}

// PreparedJournal is an internal publication proof bound to the exact queue
// generation/head used in its v1 envelope. Its fields cannot be supplied by a
// request or used to reserve a sequence before the SQL compare-and-swap.
type PreparedJournal struct {
	batch journalBatch
	base  JournalBase
	chunk JournalChunkRecord
	body  filestore.Prepared
}

func (s *DurableStore) freezeJournal(scope QueueScope, owner *QueueLease, turnID string, entries []JournalEntry) (journalBatch, error) {
	if err := scope.validate(); err != nil {
		return journalBatch{}, err
	}
	if len(entries) == 0 || len(entries) > JournalBatchEvents || len(turnID) > 191 {
		return journalBatch{}, errors.New("invalid conversation event batch")
	}
	if owner != nil && owner.Scope != scope {
		return journalBatch{}, ErrQueueOwnerLost
	}
	copyEntries := append([]JournalEntry(nil), entries...)
	for i, entry := range copyEntries {
		if entry.Type != FrameSession && entry.Type != FrameAcp || entry.Type == FrameSession && (entry.Event == "" || len(entry.Event) > 64) || entry.Type == FrameAcp && (!json.Valid(entry.Data) || owner == nil) {
			return journalBatch{}, errors.New("invalid conversation event")
		}
		if entry.At.IsZero() {
			copyEntries[i].At = s.now()
		}
	}
	// Freeze nested payloads once across CAS retries. UseNumber preserves large
	// integer payloads instead of silently rounding them through float64.
	body, err := json.Marshal(copyEntries)
	if err != nil {
		return journalBatch{}, err
	}
	var frozen []JournalEntry
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&frozen); err != nil {
		return journalBatch{}, err
	}
	batch := journalBatch{scope: scope, turnID: turnID, entries: frozen}
	if owner != nil {
		lease := *owner
		batch.owner = &lease
	}
	return batch, nil
}

// PrepareJournal freezes one batch and completes its object publication. Use
// AppendJournalWith for retries so entry timestamps/content freeze only once.
// This method refuses transaction-bound handles before any object I/O.
func (s *DurableStore) PrepareJournal(ctx context.Context, scope QueueScope, owner *QueueLease, turnID string, entries []JournalEntry) (PreparedJournal, error) {
	if err := s.outsidePayloadTransaction(); err != nil {
		return PreparedJournal{}, err
	}
	batch, err := s.freezeJournal(scope, owner, turnID, entries)
	if err != nil {
		return PreparedJournal{}, err
	}
	return s.prepareJournal(ctx, batch)
}

func (s *DurableStore) prepareJournal(ctx context.Context, batch journalBatch) (PreparedJournal, error) {
	base, err := s.ReadJournalBase(ctx, batch.scope)
	if err != nil {
		return PreparedJournal{}, err
	}
	if batch.owner != nil {
		if base.generation != batch.owner.Generation {
			return PreparedJournal{}, ErrQueueOwnerLost
		}
		if execution, ok := CurrentExecution(ctx); ok {
			if execution.Lease.Scope != batch.owner.Scope || execution.Lease.Owner != batch.owner.Owner || execution.Lease.Generation != batch.owner.Generation || execution.TurnID != batch.turnID {
				return PreparedJournal{}, ErrQueueOwnerLost
			}
			if err := execution.checkOwner(); err != nil {
				return PreparedJournal{}, err
			}
		}
	}
	if base.next > journalSafeSequence-uint64(len(batch.entries)) {
		return PreparedJournal{}, errors.New("conversation event sequence exhausted")
	}
	persisted := append([]JournalEntry(nil), batch.entries...)
	for i := range persisted {
		persisted[i].Seq, persisted[i].Generation, persisted[i].TurnID = base.next+uint64(i), base.generation, batch.turnID
	}
	chunk := JournalChunkRecord{ID: uuid.NewString(), QueueID: batch.scope.ID(), FirstSequence: base.next,
		LastSequence: base.next + uint64(len(persisted)) - 1, TurnID: batch.turnID, ExecutionEpoch: base.generation, Count: len(persisted)}
	body, err := json.Marshal(journalEnvelope{JournalVersion, chunk.QueueID, chunk.FirstSequence, chunk.LastSequence, persisted})
	if err != nil {
		return PreparedJournal{}, err
	}
	if len(body) > JournalChunkBytes {
		return PreparedJournal{}, errors.New("conversation event batch exceeds storage bound")
	}
	chunk.Bytes = len(body)
	for _, entry := range persisted {
		if entry.Type == FrameAcp {
			chunk.AcpCount++
		}
	}
	if len(persisted) == 1 && persisted[0].Type == FrameSession {
		chunk.ControlEvent = persisted[0].Event
	}
	files, err := filestore.FromDB(s.db)
	if err != nil {
		return PreparedJournal{}, err
	}
	prepared, err := files.Prepare(ctx, bytes.NewReader(body), filestore.Bytes)
	if err != nil {
		return PreparedJournal{}, err
	}
	return PreparedJournal{batch: batch, base: base, chunk: chunk, body: prepared}, nil
}

// AppendPreparedJournalTx does SQL only. A conflict must roll back the caller's
// outermost publication transaction before any body is prepared again.
func (s *DurableStore) AppendPreparedJournalTx(ctx context.Context, tx *gorm.DB, prepared PreparedJournal, validate func(*gorm.DB) error) (JournalRange, error) {
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return JournalRange{}, errors.New("prepared conversation journal publication requires an SQL transaction")
	}
	if err := ctx.Err(); err != nil {
		return JournalRange{}, err
	}
	if prepared.chunk.ID == "" || prepared.base.scope != prepared.batch.scope || prepared.chunk.Count < 1 {
		return JournalRange{}, errors.New("conversation event body was not prepared")
	}
	files, err := filestore.FromDB(tx)
	if err != nil {
		return JournalRange{}, err
	}
	ref, err := files.PreparedRef(prepared.body)
	if err != nil {
		return JournalRange{}, err
	}
	batch := prepared.batch
	queue, err := s.lockJournalTx(ctx, tx, batch.scope, batch.owner, batch.turnID, validate)
	if err != nil {
		return JournalRange{}, err
	}
	if queue.Generation != prepared.base.generation || queue.NextEventSequence != prepared.base.next {
		if batch.owner != nil && queue.Generation != batch.owner.Generation {
			return JournalRange{}, ErrQueueOwnerLost
		}
		if queue.Generation != prepared.base.generation {
			return JournalRange{}, ErrJournalPrepareConflict
		}
	}
	// A failed COMMIT response does not prove rollback. Retry the exact same
	// prepared chunk only after the current queue owner/Authority and caller's
	// validate callback above have passed; never append it again at a new head.
	var prior JournalChunkRecord
	err = tx.Table("chat_event_chunks").Take(&prior, "id = ?", prepared.chunk.ID).Error
	if err == nil {
		expected := prepared.chunk
		if prior.QueueID != expected.QueueID || prior.FirstSequence != expected.FirstSequence || prior.LastSequence != expected.LastSequence || prior.TurnID != expected.TurnID || prior.ExecutionEpoch != expected.ExecutionEpoch || prior.Count != expected.Count || prior.AcpCount != expected.AcpCount || prior.ControlEvent != expected.ControlEvent || prior.Bytes != expected.Bytes || prior.Payload != ref || queue.NextEventSequence <= expected.LastSequence {
			return JournalRange{}, ErrJournalGap
		}
		if _, err := s.lockJournalTx(ctx, tx, batch.scope, batch.owner, batch.turnID, validate); err != nil {
			return JournalRange{}, err
		}
		if err := ctx.Err(); err != nil {
			return JournalRange{}, err
		}
		return JournalRange{First: prior.FirstSequence, Last: prior.LastSequence, Count: prior.Count}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return JournalRange{}, err
	}
	if queue.NextEventSequence != prepared.base.next {
		return JournalRange{}, ErrJournalPrepareConflict
	}
	chunk := prepared.chunk
	chunk.Payload = ref
	if err := tx.Table("chat_event_chunks").Create(&chunk).Error; err != nil {
		return JournalRange{}, err
	}
	updated := tx.Model(&models.ChatQueue{}).Where("id = ? AND generation = ? AND next_event_sequence = ?", queue.ID, prepared.base.generation, prepared.base.next).
		Update("next_event_sequence", chunk.LastSequence+1)
	if updated.Error != nil {
		return JournalRange{}, updated.Error
	}
	if updated.RowsAffected != 1 {
		return JournalRange{}, ErrJournalPrepareConflict
	}
	if _, err := s.lockJournalTx(ctx, tx, batch.scope, batch.owner, batch.turnID, validate); err != nil {
		return JournalRange{}, err
	}
	if err := ctx.Err(); err != nil {
		return JournalRange{}, err
	}
	return JournalRange{First: chunk.FirstSequence, Last: chunk.LastSequence, Count: chunk.Count}, nil
}

// AppendJournalWith coordinates the entire publication, including an outer
// sandbox fence transaction. commit must return only after its outer TX has
// committed or rolled back. All PUT/GET and retry backoff run with no SQL lock.
// Failed CAS attempts leave only conservative immutable orphans, never gaps.
func (s *DurableStore) AppendJournalWith(ctx context.Context, scope QueueScope, owner *QueueLease, turnID string, entries []JournalEntry, commit func(context.Context, PreparedJournal) (JournalRange, error)) (JournalRange, error) {
	if err := s.outsidePayloadTransaction(); err != nil {
		return JournalRange{}, err
	}
	batch, err := s.freezeJournal(scope, owner, turnID, entries)
	if err != nil {
		return JournalRange{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, journalCommitTimeout)
	defer cancel()
	var prepared PreparedJournal
	var havePrepared bool
	transientFailures := 0
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return JournalRange{}, err
		}
		if !havePrepared {
			prepared, err = s.prepareJournal(ctx, batch)
			havePrepared = err == nil
		}
		if err == nil {
			var written JournalRange
			written, err = commit(ctx, prepared)
			if err == nil {
				return written, nil
			}
		}
		conflict := errors.Is(err, ErrJournalPrepareConflict)
		if !conflict {
			transientFailures++
			if !transientJournalError(err) || transientFailures >= journalTransientAttempts {
				return JournalRange{}, err
			}
		}
		// Only a definite CAS rollback permits re-preparing at a different
		// head. Transport/commit uncertainty keeps the same ID/ref/range.
		if conflict {
			havePrepared = false
		}
		delay := time.Duration(min(attempt+1, 10)) * time.Millisecond
		if !conflict {
			delay = time.Duration(1<<min(transientFailures-1, 4)) * 100 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return JournalRange{}, errors.Join(err, ctx.Err())
		case <-timer.C:
			err = nil
		}
	}
}

// AppendJournal commits its immutable body/index before the caller may publish
// a frame. ACP entries require the exact executing owner; control validation
// runs in the same short queue transaction, including after execution settles.
func (s *DurableStore) AppendJournal(ctx context.Context, scope QueueScope, owner *QueueLease, turnID string, entries []JournalEntry, validate func(*gorm.DB) error) (JournalRange, error) {
	return s.AppendJournalWith(ctx, scope, owner, turnID, entries, func(ctx context.Context, prepared PreparedJournal) (JournalRange, error) {
		var result JournalRange
		err := s.executionTransaction(ctx, scope, turnID, func(tx *gorm.DB) error {
			var err error
			result, err = s.AppendPreparedJournalTx(tx.Statement.Context, tx, prepared, validate)
			return err
		})
		return result, err
	})
}
