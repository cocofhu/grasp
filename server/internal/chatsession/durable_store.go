package chatsession

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TurnQueued    = "queued"
	TurnExecuting = "executing"
	TurnSucceeded = "succeeded"
	TurnFailed    = "failed"
	TurnCancelled = "cancelled"
	TurnUnknown   = "unknown"
)

var (
	ErrQueueOwnerLost       = errors.New("conversation execution ownership lost")
	ErrQueueOwned           = errors.New("conversation is being executed by another owner")
	ErrQueueRetired         = errors.New("conversation queue is closed")
	ErrClientConflict       = errors.New("client message identity was reused for different content")
	ErrQueueChanged         = errors.New("conversation queue changed; refresh and retry")
	ErrQueueFull            = errors.New("消息队列已满，请稍候")
	ErrQueueBusy            = errors.New("conversation has active or accepted work")
	ErrPayloadInTransaction = errors.New("conversation payload I/O requires an outer transaction-free storage handle")
	ErrPreparationDeferred  = errors.New("conversation environment is not ready; accepted message retained")
)

type DurableStore struct {
	db        *gorm.DB
	leaseTTL  time.Duration
	now       func() time.Time
	execution *executionlineage.Controller
}

func NewDurableStore(db *gorm.DB) *DurableStore {
	return &DurableStore{db: db, leaseTTL: 45 * time.Second, now: func() time.Time { return time.Now().UTC() }}
}

type QueueScope struct {
	Surface string
	Key     string
}

func (s QueueScope) ID() string {
	h := sha256.Sum256([]byte(s.Surface + "\x00" + s.Key))
	return hex.EncodeToString(h[:])
}

func (s QueueScope) validate() error {
	if s.Surface == "" || len(s.Surface) > 64 || s.Key == "" || len(s.Key) > 191 || strings.ContainsAny(s.Surface+s.Key, "\x00\r\n") {
		return errors.New("invalid conversation queue scope")
	}
	return nil
}

type QueueIntent struct {
	ID              string
	ActorID         string
	ClientMessageID string
	Fingerprint     string
	Payload         []byte
	Display         []byte
}

// PreparedIntent cannot be constructed from an inbound reference. PrepareIntent
// freezes its content and completes storage publication before SQL admission.
type PreparedIntent struct {
	scope  QueueScope
	intent QueueIntent
	body   filestore.Prepared
}

func (s *DurableStore) outsidePayloadTransaction() error {
	if _, inside := s.db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return ErrPayloadInTransaction
	}
	return nil
}

func (s *DurableStore) PrepareIntent(ctx context.Context, scope QueueScope, intent QueueIntent) (PreparedIntent, error) {
	if err := s.outsidePayloadTransaction(); err != nil {
		return PreparedIntent{}, err
	}
	if err := scope.validate(); err != nil {
		return PreparedIntent{}, err
	}
	if err := validateIntent(intent); err != nil {
		return PreparedIntent{}, err
	}
	files, err := filestore.FromDB(s.db)
	if err != nil {
		return PreparedIntent{}, err
	}
	// The caller no longer owns mutable payload/display buffers after this
	// boundary. A prepared receipt cannot later acquire different bytes.
	payload := append([]byte(nil), intent.Payload...)
	intent.Payload = nil
	intent.Display = append([]byte(nil), intent.Display...)
	body, err := files.Prepare(ctx, bytes.NewReader(payload), filestore.Bytes)
	if err != nil {
		return PreparedIntent{}, err
	}
	return PreparedIntent{scope: scope, intent: intent, body: body}, nil
}

type QueueReceipt struct {
	Turn       models.ChatQueueTurn
	Waiting    int
	Duplicate  bool
	Generation uint64
	Revision   uint64
}

func clientKey(actor, clientID string) string {
	hash := sha256.Sum256([]byte(actor + "\x00" + clientID))
	return hex.EncodeToString(hash[:])
}

type DurableSnapshot struct {
	Queue   models.ChatQueue
	Active  *models.ChatQueueTurn
	Pending []models.ChatQueueTurn
}

// Metadata supports conversation lists without loading accepted message bodies
// from object storage or constructing a local execution controller.
func (s *DurableStore) Metadata(ctx context.Context, scope QueueScope) (Snapshot, error) {
	var result Snapshot
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var queue models.ChatQueue
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&queue, "id = ?", scope.ID()).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		var waiting int64
		if err := tx.Model(&models.ChatQueueTurn{}).Where("queue_id = ? AND state = ?", queue.ID, TurnQueued).Count(&waiting).Error; err != nil {
			return err
		}
		result = Snapshot{SessionGeneration: queue.Generation, Revision: queue.Revision, Busy: queue.ActiveTurnID != "", Waiting: int(waiting)}
		return nil
	})
	return result, err
}

// lock reserves the same SQL row for admission, claim, cancellation and finish.
// Revision always changes, including MySQL's no-op UPDATE affected-row mode.
func (s *DurableStore) lock(tx *gorm.DB, scope QueueScope) (*models.ChatQueue, error) {
	if err := scope.validate(); err != nil {
		return nil, err
	}
	initial := models.ChatQueue{ID: scope.ID(), Surface: scope.Surface, SessionKey: scope.Key}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&models.ChatQueue{}).Where("id = ?", initial.ID).
		UpdateColumn("revision", gorm.Expr("revision + 1")).Error; err != nil {
		return nil, err
	}
	var queue models.ChatQueue
	if err := tx.First(&queue, "id = ?", initial.ID).Error; err != nil {
		return nil, err
	}
	if queue.Generation == 0 {
		epoch, err := s.nextEpoch(tx)
		if err != nil {
			return nil, err
		}
		queue.Generation = epoch
		if err := tx.Model(&queue).Update("generation", epoch).Error; err != nil {
			return nil, err
		}
	}
	return &queue, nil
}

func (s *DurableStore) nextEpoch(tx *gorm.DB) (uint64, error) {
	seed := s.now().UnixMilli()
	if seed < 1 {
		seed = 1
	}
	epoch := models.ChatQueueEpoch{ID: "global", Generation: uint64(seed)}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&epoch).Error; err != nil {
		return 0, err
	}
	if err := tx.Model(&epoch).UpdateColumn("generation", gorm.Expr("generation + 1")).Error; err != nil {
		return 0, err
	}
	if err := tx.First(&epoch, "id = ?", "global").Error; err != nil {
		return 0, err
	}
	return epoch.Generation, nil
}

func validateIntent(intent QueueIntent) error {
	if intent.ID == "" || len(intent.ID) > 191 || strings.ContainsAny(intent.ID, "\x00\r\n") {
		return errors.New("invalid conversation turn identity")
	}
	if intent.ActorID == "" || len(intent.ActorID) > 191 || intent.ClientMessageID == "" || len(intent.ClientMessageID) > 128 ||
		strings.ContainsAny(intent.ActorID+intent.ClientMessageID, "\x00\r\n") || len(intent.Fingerprint) != 64 || len(intent.Payload) == 0 {
		return errors.New("invalid conversation intent")
	}
	_, err := hex.DecodeString(intent.Fingerprint)
	if err != nil {
		return err
	}
	return validateQueueDisplay(intent.Display)
}

// Admit returns success only after immutable payload and SQL receipt commit.
// validate runs under the queue lock so a concurrent close cannot accept a late
// message. A duplicate returns its existing receipt, including terminal ones.
func (s *DurableStore) Admit(ctx context.Context, scope QueueScope, intent QueueIntent, capacity int, validate func(*gorm.DB) error) (QueueReceipt, error) {
	var receipt QueueReceipt
	if err := s.outsidePayloadTransaction(); err != nil {
		return receipt, err
	}
	if err := validateIntent(intent); err != nil {
		return receipt, err
	}
	// Already committed receipts need no upload, including after close and
	// after a lost acknowledgement. The authoritative check repeats below.
	if previous, err := s.Existing(ctx, scope, intent.ActorID, intent.ClientMessageID, intent.Fingerprint); err != nil {
		return receipt, err
	} else if previous != nil {
		err = s.executionTransaction(ctx, scope, previous.ID, func(tx *gorm.DB) error {
			var lookupErr error
			receipt, lookupErr = s.admitPreparedTx(tx, scope, intent, "", capacity, validate)
			return lookupErr
		})
		return receipt, err
	}
	prepared, err := s.PrepareIntent(ctx, scope, intent)
	if err != nil {
		return receipt, err
	}
	err = s.executionTransaction(ctx, scope, intent.ID, func(tx *gorm.DB) error {
		var admitErr error
		receipt, admitErr = s.AdmitPreparedTx(tx, scope, prepared, capacity, validate)
		return admitErr
	})
	return receipt, err
}

// AdmitPreparedTx performs only SQL. Callers with a surrounding creation or
// lifecycle transaction must prepare before entering that outer transaction.
func (s *DurableStore) AdmitPreparedTx(tx *gorm.DB, scope QueueScope, prepared PreparedIntent, capacity int, validate func(*gorm.DB) error) (QueueReceipt, error) {
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return QueueReceipt{}, errors.New("prepared conversation admission requires an SQL transaction")
	}
	if prepared.scope != scope || prepared.body.Size() < 1 {
		return QueueReceipt{}, errors.New("conversation intent was not prepared for this scope")
	}
	if err := s.guardExecutionTx(tx, scope, prepared.intent.ID); err != nil {
		return QueueReceipt{}, err
	}
	files, err := filestore.FromDB(tx)
	if err != nil {
		return QueueReceipt{}, err
	}
	ref, err := files.PreparedRef(prepared.body)
	if err != nil {
		return QueueReceipt{}, err
	}
	return s.admitPreparedTx(tx, scope, prepared.intent, ref, capacity, validate)
}

func (s *DurableStore) admitPreparedTx(tx *gorm.DB, scope QueueScope, intent QueueIntent, rawRef string, capacity int, validate func(*gorm.DB) error) (QueueReceipt, error) {
	var receipt QueueReceipt
	if err := s.guardExecutionTx(tx, scope, intent.ID); err != nil {
		return receipt, err
	}
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	err := func() error {
		queue, err := s.lock(tx, scope)
		if err != nil {
			return err
		}
		var previous models.ChatQueueTurn
		err = tx.Omit("payload").Where("queue_id = ? AND client_key = ?", queue.ID, clientKey(intent.ActorID, intent.ClientMessageID)).First(&previous).Error
		if err == nil {
			if previous.Fingerprint != intent.Fingerprint {
				return ErrClientConflict
			}
			receipt.Turn, receipt.Duplicate = previous, true
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		} else {
			if rawRef == "" {
				return ErrQueueChanged
			}
			if validate != nil {
				if err := validate(tx); err != nil {
					return err
				}
			}
			if queue.Retired {
				return ErrQueueRetired
			}
			var waiting int64
			if err := tx.Model(&models.ChatQueueTurn{}).Where("queue_id = ? AND state = ?", queue.ID, TurnQueued).Count(&waiting).Error; err != nil {
				return err
			}
			if waiting >= int64(capacity) {
				return ErrQueueFull
			}
			queue.NextSequence++
			receipt.Turn = models.ChatQueueTurn{ID: intent.ID, QueueID: queue.ID, ActorID: intent.ActorID,
				ClientMessageID: intent.ClientMessageID, ClientKey: clientKey(intent.ActorID, intent.ClientMessageID), Fingerprint: intent.Fingerprint, Sequence: queue.NextSequence,
				Position: queue.NextSequence, State: TurnQueued, Display: string(intent.Display)}
			if err := s.insertQueueTurn(tx, &receipt.Turn, rawRef); err != nil {
				return err
			}
			if err := tx.Model(queue).Update("next_sequence", queue.NextSequence).Error; err != nil {
				return err
			}
		}
		var waiting int64
		if err := tx.Model(&models.ChatQueueTurn{}).Where("queue_id = ? AND state = ?", queue.ID, TurnQueued).Count(&waiting).Error; err != nil {
			return err
		}
		receipt.Waiting, receipt.Generation, receipt.Revision = int(waiting), queue.Generation, queue.Revision
		return nil
	}()
	return receipt, err
}

func (s *DurableStore) insertQueueTurn(tx *gorm.DB, turn *models.ChatQueueTurn, rawRef string) error {
	if turn.CreatedAt.IsZero() {
		turn.CreatedAt = s.now()
	}
	turn.UpdatedAt = s.now()
	// A table/map insert has no payload serializer. Keep the existing column
	// and fileref format so legacy readers and the reference catalogue work.
	values := map[string]any{"id": turn.ID, "queue_id": turn.QueueID, "actor_id": turn.ActorID,
		"client_message_id": turn.ClientMessageID, "client_key": turn.ClientKey, "fingerprint": turn.Fingerprint,
		"sequence": turn.Sequence, "position": turn.Position, "state": turn.State, "payload": rawRef,
		"display": turn.Display, "execution_epoch": turn.ExecutionEpoch, "dispatched_at": turn.DispatchedAt,
		"started_at": turn.StartedAt, "finished_at": turn.FinishedAt, "reason": turn.Reason,
		"result_id": turn.ResultID, "created_at": turn.CreatedAt, "updated_at": turn.UpdatedAt}
	return tx.Table("chat_queue_turns").Create(&values).Error
}

// Existing supports deduplication before attachment ingestion creates new blob
// IDs. The authenticated caller still checks access to the owning conversation.
func (s *DurableStore) Existing(ctx context.Context, scope QueueScope, actor, clientID, fingerprint string) (*models.ChatQueueTurn, error) {
	var row models.ChatQueueTurn
	err := s.db.WithContext(ctx).Omit("payload").Where("queue_id = ? AND client_key = ?", scope.ID(), clientKey(actor, clientID)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if row.Fingerprint != fingerprint {
		return nil, ErrClientConflict
	}
	return &row, nil
}

func (s *DurableStore) Snapshot(ctx context.Context, scope QueueScope) (DurableSnapshot, error) {
	return s.snapshot(ctx, scope, false)
}

// SnapshotMetadata returns the same authoritative ordered set without loading
// execution bodies. Inline display previews are enough for status frames.
func (s *DurableStore) SnapshotMetadata(ctx context.Context, scope QueueScope) (DurableSnapshot, error) {
	return s.snapshot(ctx, scope, true)
}

func (s *DurableStore) snapshot(ctx context.Context, scope QueueScope, metadataOnly bool) (DurableSnapshot, error) {
	var result DurableSnapshot
	if !metadataOnly {
		if err := s.outsidePayloadTransaction(); err != nil {
			return result, err
		}
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&result.Queue, "id = ?", scope.ID()).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				result.Queue = models.ChatQueue{ID: scope.ID(), Surface: scope.Surface, SessionKey: scope.Key}
				return nil
			}
			return err
		}
		var turns []models.ChatQueueTurn
		query := tx.Omit("payload").Where("queue_id = ? AND state IN ?", scope.ID(), []string{TurnQueued, TurnExecuting}).Order("position, sequence, id")
		if err := query.Find(&turns).Error; err != nil {
			return err
		}
		result.Pending = []models.ChatQueueTurn{}
		for _, turn := range turns {
			if turn.ID == result.Queue.ActiveTurnID && turn.State == TurnExecuting {
				copy := turn
				result.Active = &copy
			} else if turn.State == TurnQueued {
				result.Pending = append(result.Pending, turn)
			} else {
				return errors.New("conversation active receipt disagrees with its queue")
			}
		}
		if result.Queue.ActiveTurnID != "" && result.Active == nil {
			return errors.New("conversation active receipt is missing")
		}
		return nil
	})
	if err == nil && !metadataOnly {
		for i := range result.Pending {
			if err = s.loadTurnBody(ctx, scope, &result.Pending[i]); err != nil {
				return DurableSnapshot{}, err
			}
		}
		if result.Active != nil {
			err = s.loadTurnBody(ctx, scope, result.Active)
		}
	}
	return result, err
}

// MySQL TEXT is limited to 65,535 bytes, including multibyte UTF-8 characters.
const MaxQueueDisplayBytes = 64*1024 - 1

func validateQueueDisplay(display []byte) error {
	if len(display) > MaxQueueDisplayBytes || len(display) > 0 && !json.Valid(display) {
		return errors.New("invalid conversation display preview")
	}
	return nil
}

// ReadTurn is scoped and loads exactly one immutable body outside a queue
// transaction. Legacy preview backfill must not hold an SQL lock during S3 I/O.
func (s *DurableStore) ReadTurn(ctx context.Context, scope QueueScope, id string) (models.ChatQueueTurn, error) {
	var row models.ChatQueueTurn
	if err := s.outsidePayloadTransaction(); err != nil {
		return row, err
	}
	err := s.db.WithContext(ctx).Omit("payload").First(&row, "id = ? AND queue_id = ?", id, scope.ID()).Error
	if err == nil {
		err = s.loadTurnBody(ctx, scope, &row)
	}
	return row, err
}

func rawQueueBody(query *gorm.DB) (string, error) {
	var cell struct{ Payload string }
	// A raw destination has no serializer. Take returns after its SQL cursor
	// has closed; object I/O only begins in the caller afterwards.
	err := query.Select("payload").Take(&cell).Error
	return cell.Payload, err
}

func (s *DurableStore) loadTurnBody(ctx context.Context, scope QueueScope, row *models.ChatQueueTurn) error {
	if err := s.outsidePayloadTransaction(); err != nil {
		return err
	}
	if row.QueueID != scope.ID() {
		return ErrQueueChanged
	}
	raw, err := rawQueueBody(s.db.WithContext(ctx).Table("chat_queue_turns").Where("id = ? AND queue_id = ? AND fingerprint = ?", row.ID, row.QueueID, row.Fingerprint))
	if err != nil {
		return err
	}
	files, err := filestore.FromDB(s.db)
	if err != nil {
		return err
	}
	row.Payload, err = files.Load(ctx, raw, filestore.Bytes)
	return err
}

// BackfillDisplay only fills an absent preview for the same immutable receipt.
// Concurrent readers never replace each other's display or rewrite the body.
func (s *DurableStore) BackfillDisplay(ctx context.Context, scope QueueScope, id, fingerprint string, display []byte) error {
	if len(display) == 0 {
		return errors.New("conversation display preview required")
	}
	if err := validateQueueDisplay(display); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&models.ChatQueueTurn{}).
		Where("id = ? AND queue_id = ? AND fingerprint = ? AND (display IS NULL OR display = '')", id, scope.ID(), fingerprint).
		Update("display", string(display)).Error
}

type QueueLease struct {
	Scope      QueueScope
	Owner      string
	Generation uint64
	ExpiresAt  time.Time
}

// Acquire takes execution ownership. An expired, dispatched turn becomes
// unknown, never queued again. A preparation-only turn may safely resume.
// recovered is surfaced to the user before a later accepted turn starts.
func (s *DurableStore) Acquire(ctx context.Context, scope QueueScope, owner string) (lease QueueLease, recovered *models.ChatQueueTurn, err error) {
	if _, err := uuid.Parse(owner); err != nil {
		return lease, nil, errors.New("invalid conversation executor identity")
	}
	err = s.executionTransaction(ctx, scope, "", func(tx *gorm.DB) error {
		queue, err := s.lock(tx, scope)
		if err != nil {
			return err
		}
		now := s.now()
		if queue.Retired {
			return ErrQueueRetired
		}
		if queue.Owner != "" && queue.OwnerExpiresAt != nil && queue.OwnerExpiresAt.After(now) {
			if queue.Owner != owner {
				return ErrQueueOwned
			}
			lease = QueueLease{scope, owner, queue.Generation, *queue.OwnerExpiresAt}
			return nil
		}
		recovered, err = s.recoverActive(tx, queue)
		if err != nil {
			return err
		}
		epoch, err := s.nextEpoch(tx)
		if err != nil {
			return err
		}
		expires := now.Add(s.leaseTTL)
		if err := tx.Model(queue).Updates(map[string]any{"owner": owner, "owner_expires_at": expires,
			"generation": epoch, "active_turn_id": "", "cancel_requested": false}).Error; err != nil {
			return err
		}
		lease = QueueLease{scope, owner, epoch, expires}
		return nil
	})
	return lease, recovered, err
}

func (s *DurableStore) recoverActive(tx *gorm.DB, queue *models.ChatQueue) (*models.ChatQueueTurn, error) {
	// An executing receipt outside the active pointer has no trustworthy
	// preparation-only proof. Quarantine it under this queue's lock; never
	// repair another conversation, and never make uncertain effects replayable.
	orphans := tx.Model(&models.ChatQueueTurn{}).Where("queue_id = ? AND state = ? AND id <> ?", queue.ID, TurnExecuting, queue.ActiveTurnID).
		Updates(map[string]any{"state": TurnUnknown, "reason": "active_receipt_inconsistent", "finished_at": s.now()})
	if orphans.Error != nil {
		return nil, orphans.Error
	}
	if orphans.RowsAffected > 0 {
		log.Warn().Str("queue", queue.ID).Int64("receipts", orphans.RowsAffected).Msg("orphan conversation executions retained as unknown; automatic replay refused")
	}
	if queue.ActiveTurnID == "" {
		return nil, nil
	}
	var active models.ChatQueueTurn
	// Read the metadata first. A stale pointer must not prevent later accepted
	// work from running, or require an unrelated/missing payload to be loaded.
	if err := tx.Omit("payload").First(&active, "id = ? AND queue_id = ?", queue.ActiveTurnID, queue.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Warn().Str("queue", queue.ID).Str("turn", queue.ActiveTurnID).Msg("conversation active receipt is missing; stale pointer cleared")
			return nil, nil
		}
		return nil, err
	}
	if active.State != TurnExecuting {
		log.Warn().Str("queue", queue.ID).Str("turn", active.ID).Str("state", active.State).Msg("conversation active receipt disagrees with queue; automatic replay refused")
		if !terminalState(active.State) {
			if err := tx.Model(&models.ChatQueueTurn{}).Where("id = ? AND queue_id = ?", active.ID, queue.ID).
				Updates(map[string]any{"state": TurnUnknown, "reason": "active_receipt_inconsistent", "finished_at": s.now()}).Error; err != nil {
				return nil, err
			}
		}
		return nil, nil
	}
	var hasPayload int64
	if err := tx.Table("chat_queue_turns").Where("id = ? AND queue_id = ? AND payload IS NOT NULL AND payload <> ''", active.ID, queue.ID).Count(&hasPayload).Error; err != nil {
		return nil, err
	}
	state, reason := TurnQueued, ""
	var finished *time.Time
	now := s.now()
	if active.DispatchedAt != nil {
		state, reason, finished = TurnUnknown, "executor_lost", &now
	} else if queue.CancelRequested || queue.Retired {
		state, reason, finished = TurnCancelled, "cancelled_before_dispatch", &now
	} else if hasPayload == 0 {
		state, reason, finished = TurnUnknown, "accepted_payload_missing", &now
	}
	updates := map[string]any{"state": state, "reason": reason, "finished_at": finished}
	if state == TurnQueued {
		// Zero is reserved for an interrupted preparation. Reorder numbers only
		// pending work from one, so a requeued active command stays at the head.
		updates["position"] = 0
		active.Position = 0
	}
	if err := tx.Model(&active).Updates(updates).Error; err != nil {
		return nil, err
	}
	active.State, active.Reason, active.FinishedAt = state, reason, finished
	return &active, nil
}

// ReconcileRetired releases a crashed executor's closed queue without opening
// an execution lane. A running owner still gets time to cancel native work.
func (s *DurableStore) ReconcileRetired(ctx context.Context, surface string) error {
	var rows []models.ChatQueue
	if err := s.executableQueues(s.db.WithContext(ctx)).Where("surface = ? AND retired = ? AND (active_turn_id <> '' OR EXISTS (SELECT 1 FROM chat_queue_turns t WHERE t.queue_id = chat_queues.id AND t.state = ?)) AND (owner_expires_at IS NULL OR owner_expires_at <= ?)", surface, true, TurnExecuting, s.now()).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if err := s.executionTransaction(ctx, QueueScope{row.Surface, row.SessionKey}, "", func(tx *gorm.DB) error {
			queue, err := s.lock(tx, QueueScope{row.Surface, row.SessionKey})
			if err != nil {
				return err
			}
			if !queue.Retired || queue.OwnerExpiresAt != nil && queue.OwnerExpiresAt.After(s.now()) {
				return nil
			}
			if _, err := s.recoverActive(tx, queue); err != nil {
				return err
			}
			return tx.Model(queue).Updates(map[string]any{"active_turn_id": "", "owner": "", "owner_expires_at": nil}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *DurableStore) owned(queue *models.ChatQueue, lease QueueLease) bool {
	return queue.Owner == lease.Owner && queue.Generation == lease.Generation && queue.OwnerExpiresAt != nil && queue.OwnerExpiresAt.After(s.now())
}

func (s *DurableStore) Renew(ctx context.Context, lease QueueLease) error {
	_, err := s.RenewUntil(ctx, lease)
	return err
}

// RenewUntil returns the exact deadline committed by this renewal. A worker
// can retry transient errors without extending the last confirmed lease.
func (s *DurableStore) RenewUntil(ctx context.Context, lease QueueLease) (time.Time, error) {
	now := s.now()
	expires := now.Add(s.leaseTTL)
	err := s.executionTransaction(ctx, lease.Scope, "", func(tx *gorm.DB) error {
		result := tx.Model(&models.ChatQueue{}).
			Where("id = ? AND owner = ? AND generation = ? AND owner_expires_at > ?", lease.Scope.ID(), lease.Owner, lease.Generation, now).
			Update("owner_expires_at", expires)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrQueueOwnerLost
		}
		return nil
	})
	if err != nil {
		return time.Time{}, err
	}
	return expires, nil
}

func (s *DurableStore) Claim(ctx context.Context, lease QueueLease) (*models.ChatQueueTurn, error) {
	var turn *models.ChatQueueTurn
	if err := s.outsidePayloadTransaction(); err != nil {
		return nil, err
	}
	err := s.executionTransaction(ctx, lease.Scope, "", func(tx *gorm.DB) error {
		queue, err := s.lock(tx, lease.Scope)
		if err != nil {
			return err
		}
		if !s.owned(queue, lease) {
			return ErrQueueOwnerLost
		}
		if queue.Retired || queue.ActiveTurnID != "" {
			return ErrQueueChanged
		}
		var row models.ChatQueueTurn
		for {
			row = models.ChatQueueTurn{}
			err = tx.Omit("payload").Where("queue_id = ? AND state = ?", queue.ID, TurnQueued).Order("position, sequence, id").First(&row).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			if row.DispatchedAt == nil {
				break
			}
			// A partially migrated or manually edited state must never erase
			// existing dispatch proof and grant permission for a second effect.
			if err := tx.Model(&models.ChatQueueTurn{}).Where("id = ? AND queue_id = ?", row.ID, queue.ID).
				Updates(map[string]any{"state": TurnUnknown, "reason": "queued_dispatch_inconsistent", "finished_at": s.now()}).Error; err != nil {
				return err
			}
			log.Warn().Str("queue", queue.ID).Str("turn", row.ID).Msg("queued conversation dispatch retained as unknown; automatic replay refused")
		}
		now := s.now()
		if err := tx.Model(&row).Updates(map[string]any{"state": TurnExecuting, "execution_epoch": lease.Generation,
			"started_at": now, "dispatched_at": nil}).Error; err != nil {
			return err
		}
		if err := tx.Model(queue).Updates(map[string]any{"active_turn_id": row.ID, "cancel_requested": false}).Error; err != nil {
			return err
		}
		row.State, row.ExecutionEpoch, row.StartedAt, row.DispatchedAt = TurnExecuting, lease.Generation, &now, nil
		turn = &row
		return nil
	})
	if err == nil && turn != nil {
		err = s.loadTurnBody(ctx, lease.Scope, turn)
		if err == nil {
			var cancelled bool
			cancelled, err = s.Cancellation(ctx, lease, turn.ID)
			if err == nil && cancelled {
				err = context.Canceled
			}
		}
		if err != nil {
			// No dispatch has happened. Return the preparation only while the
			// same fence still owns it; concurrent cancellation/close wins.
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = s.DeferPreparation(cleanup, lease, turn.ID)
			cancel()
			return nil, err
		}
	}
	return turn, err
}

// Fence must be called in the same transaction as user/assistant publication.
// Even a late successful native result cannot overwrite a successor's state.
func (s *DurableStore) Fence(tx *gorm.DB, lease QueueLease, turnID string) error {
	if err := s.guardExecutionTx(tx, lease.Scope, turnID); err != nil {
		return err
	}
	queue, err := s.lock(tx, lease.Scope)
	if err != nil {
		return err
	}
	if !s.owned(queue, lease) || queue.ActiveTurnID != turnID {
		return ErrQueueOwnerLost
	}
	var count int64
	if err := tx.Model(&models.ChatQueueTurn{}).Where("id = ? AND queue_id = ? AND state = ? AND execution_epoch = ?", turnID, queue.ID, TurnExecuting, lease.Generation).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return ErrQueueOwnerLost
	}
	return nil
}

func (s *DurableStore) Dispatch(ctx context.Context, lease QueueLease, turnID string) error {
	return s.executionTransaction(ctx, lease.Scope, turnID, func(tx *gorm.DB) error {
		if err := s.Fence(tx, lease, turnID); err != nil {
			return err
		}
		var queue models.ChatQueue
		if err := tx.Select("cancel_requested", "retired").First(&queue, "id = ?", lease.Scope.ID()).Error; err != nil {
			return err
		}
		if queue.CancelRequested || queue.Retired {
			return context.Canceled
		}
		return tx.Model(&models.ChatQueueTurn{}).Where("id = ? AND dispatched_at IS NULL", turnID).Update("dispatched_at", s.now()).Error
	})
}

func (s *DurableStore) Receipt(ctx context.Context, scope QueueScope, id string) (*models.ChatQueueTurn, error) {
	var row models.ChatQueueTurn
	err := s.db.WithContext(ctx).Omit("payload").First(&row, "id = ? AND queue_id = ?", id, scope.ID()).Error
	return &row, err
}

func (s *DurableStore) Waiting(ctx context.Context, scope QueueScope) (int, error) {
	var count int64
	err := s.db.WithContext(ctx).Model(&models.ChatQueueTurn{}).Where("queue_id = ? AND state = ?", scope.ID(), TurnQueued).Count(&count).Error
	return int(count), err
}

// Abandon permits recovery only after this local executor has actually stopped.
// Active dispatches remain recorded and are classified by the next owner.
func (s *DurableStore) Abandon(ctx context.Context, lease QueueLease) error {
	return s.executionTransaction(ctx, lease.Scope, "", func(tx *gorm.DB) error {
		return tx.Model(&models.ChatQueue{}).
			Where("id = ? AND owner = ? AND generation = ?", lease.Scope.ID(), lease.Owner, lease.Generation).
			Update("owner_expires_at", s.now()).Error
	})
}

func terminalState(state string) bool {
	return state == TurnSucceeded || state == TurnFailed || state == TurnCancelled || state == TurnUnknown
}

// Finish belongs in the same transaction as the final persisted message.
func (s *DurableStore) Finish(tx *gorm.DB, lease QueueLease, turnID, state, reason, resultID string) error {
	if !terminalState(state) || len(reason) > 64 || len(resultID) > 191 {
		return errors.New("invalid conversation result receipt")
	}
	if err := s.Fence(tx, lease, turnID); err != nil {
		return err
	}
	if err := tx.Model(&models.ChatQueueTurn{}).Where("id = ?", turnID).Updates(map[string]any{
		"state": state, "reason": reason, "result_id": resultID, "finished_at": s.now()}).Error; err != nil {
		return err
	}
	return tx.Model(&models.ChatQueue{}).Where("id = ?", lease.Scope.ID()).Updates(map[string]any{"active_turn_id": "", "cancel_requested": false}).Error
}

// DeferPreparation returns only an undispatched command to the queue head,
// ahead of reordered waiters. An explicit stop/close settles it as cancelled.
func (s *DurableStore) DeferPreparation(ctx context.Context, lease QueueLease, turnID string) error {
	return s.executionTransaction(ctx, lease.Scope, turnID, func(tx *gorm.DB) error {
		if err := s.Fence(tx, lease, turnID); err != nil {
			return err
		}
		var row models.ChatQueueTurn
		if err := tx.Omit("payload").First(&row, "id = ?", turnID).Error; err != nil {
			return err
		}
		if row.DispatchedAt != nil {
			return ErrQueueChanged
		}
		var queue models.ChatQueue
		if err := tx.First(&queue, "id = ?", lease.Scope.ID()).Error; err != nil {
			return err
		}
		if queue.CancelRequested || queue.Retired {
			return s.Finish(tx, lease, turnID, TurnCancelled, "user_cancelled", "")
		}
		if err := tx.Model(&row).Updates(map[string]any{"state": TurnQueued, "reason": "environment_not_ready", "started_at": nil, "position": 0}).Error; err != nil {
			return err
		}
		return tx.Model(&queue).Update("active_turn_id", "").Error
	})
}

func (s *DurableStore) Release(ctx context.Context, lease QueueLease) error {
	return s.executionTransaction(ctx, lease.Scope, "", func(tx *gorm.DB) error {
		queue, err := s.lock(tx, lease.Scope)
		if err != nil {
			return err
		}
		if !s.owned(queue, lease) || queue.ActiveTurnID != "" {
			return ErrQueueOwnerLost
		}
		return tx.Model(queue).Updates(map[string]any{"owner": "", "owner_expires_at": nil}).Error
	})
}

func (s *DurableStore) Cancel(ctx context.Context, scope QueueScope, activeID string, clear bool) (bool, error) {
	matched := false
	err := s.executionTransaction(ctx, scope, activeID, func(tx *gorm.DB) error {
		queue, err := s.lock(tx, scope)
		if err != nil {
			return err
		}
		if activeID != "" && queue.ActiveTurnID != activeID {
			return nil
		}
		matched = true
		if clear {
			if err := tx.Model(&models.ChatQueueTurn{}).Where("queue_id = ? AND state = ?", queue.ID, TurnQueued).
				Updates(map[string]any{"state": TurnCancelled, "reason": "user_cancelled", "finished_at": s.now()}).Error; err != nil {
				return err
			}
		}
		if queue.ActiveTurnID != "" {
			return tx.Model(queue).Update("cancel_requested", true).Error
		}
		return nil
	})
	return matched, err
}

func (s *DurableStore) Cancellation(ctx context.Context, lease QueueLease, turnID string) (bool, error) {
	var queue models.ChatQueue
	if err := s.db.WithContext(ctx).First(&queue, "id = ?", lease.Scope.ID()).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, ErrQueueOwnerLost
		}
		return false, err
	}
	if !s.owned(&queue, lease) || queue.ActiveTurnID != turnID {
		return false, ErrQueueOwnerLost
	}
	return queue.CancelRequested || queue.Retired, nil
}

func (s *DurableStore) Remove(ctx context.Context, scope QueueScope, id string) error {
	return s.executionTransaction(ctx, scope, id, func(tx *gorm.DB) error {
		queue, err := s.lock(tx, scope)
		if err != nil {
			return err
		}
		result := tx.Model(&models.ChatQueueTurn{}).Where("id = ? AND queue_id = ? AND state = ?", id, queue.ID, TurnQueued).
			Updates(map[string]any{"state": TurnCancelled, "reason": "user_removed", "finished_at": s.now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		return nil
	})
}

func (s *DurableStore) Reorder(ctx context.Context, scope QueueScope, ids []string) error {
	return s.executionTransaction(ctx, scope, "", func(tx *gorm.DB) error {
		queue, err := s.lock(tx, scope)
		if err != nil {
			return err
		}
		var rows []models.ChatQueueTurn
		if err := tx.Select("id").Where("queue_id = ? AND state = ?", queue.ID, TurnQueued).Find(&rows).Error; err != nil {
			return err
		}
		if len(ids) == 0 || len(ids) != len(rows) {
			return ErrQueueChanged
		}
		remaining := map[string]bool{}
		for _, row := range rows {
			remaining[row.ID] = true
		}
		for i, id := range ids {
			if !remaining[id] {
				return ErrQueueChanged
			}
			delete(remaining, id)
			if err := tx.Model(&models.ChatQueueTurn{}).Where("id = ?", id).Update("position", i+1).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// Retire is composed with the owning session's explicit close transaction.
func (s *DurableStore) Retire(tx *gorm.DB, scope QueueScope) error {
	if err := s.guardExecutionTx(tx, scope, ""); err != nil {
		return err
	}
	queue, err := s.lock(tx, scope)
	if err != nil {
		return err
	}
	if err := tx.Model(&models.ChatQueueTurn{}).Where("queue_id = ? AND state = ?", queue.ID, TurnQueued).
		Updates(map[string]any{"state": TurnCancelled, "reason": "session_closed", "finished_at": s.now()}).Error; err != nil {
		return err
	}
	return tx.Model(queue).Updates(map[string]any{"retired": true, "cancel_requested": true}).Error
}

func (s *DurableStore) Watermark(ctx context.Context) (uint64, error) {
	var row models.ChatQueueEpoch
	err := s.db.WithContext(ctx).First(&row, "id = ?", "global").Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	return row.Generation, err
}

func (s *DurableStore) ReadyScopes(ctx context.Context, surface string) ([]QueueScope, error) {
	var queues []models.ChatQueue
	err := s.executableQueues(s.db.WithContext(ctx)).Where("surface = ? AND retired = ? AND (owner = '' OR owner_expires_at IS NULL OR owner_expires_at <= ?) AND (active_turn_id != '' OR EXISTS (SELECT 1 FROM chat_queue_turns t WHERE t.queue_id = chat_queues.id AND t.state IN ?))", surface, false, s.now(), []string{TurnQueued, TurnExecuting}).
		Order("created_at, id").Find(&queues).Error
	if err != nil {
		return nil, err
	}
	out := make([]QueueScope, 0, len(queues))
	for _, queue := range queues {
		out = append(out, QueueScope{queue.Surface, queue.SessionKey})
	}
	return out, nil
}

// GuardOpen serializes initial environment publication with admission and
// retirement. Queued or active work is allowed: this only locks queue metadata,
// and never reads, decodes or loads a turn's accepted payload.
func (s *DurableStore) GuardOpen(tx *gorm.DB, scope QueueScope) error {
	if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return errors.New("conversation publication guard requires an SQL transaction")
	}
	if err := s.guardExecutionTx(tx, scope, ""); err != nil {
		return err
	}
	queue, err := s.lock(tx, scope)
	if err != nil {
		return err
	}
	if queue.Retired {
		return ErrQueueRetired
	}
	return nil
}

// GuardIdle composes with an environment stop transaction. Its queue lock is
// shared by Admit and Claim: accepted work wins over Stop, and a committed
// stopped environment is visible to the later Admit validation callback.
func (s *DurableStore) GuardIdle(tx *gorm.DB, scope QueueScope) error {
	if err := s.guardExecutionTx(tx, scope, ""); err != nil {
		return err
	}
	queue, err := s.lock(tx, scope)
	if err != nil {
		return err
	}
	if queue.Retired {
		return ErrQueueRetired
	}
	if queue.ActiveTurnID != "" {
		return ErrQueueBusy
	}
	var count int64
	if err := tx.Model(&models.ChatQueueTurn{}).Where("queue_id = ? AND state IN ?", queue.ID, []string{TurnQueued, TurnExecuting}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrQueueBusy
	}
	return nil
}

// ImportLegacy preserves receipt IDs and raw bytes. Started legacy work has no
// trustworthy dispatch marker, so it becomes unknown rather than being rerun.
// Source rows are removed only in the transaction that creates every receipt.
func (s *DurableStore) ImportLegacy(ctx context.Context, scope QueueScope, actor string, classify ...func(*gorm.DB, models.ChatPendingTurn) (string, string, error)) ([]models.ChatQueueTurn, error) {
	var imported []models.ChatQueueTurn
	if err := s.outsidePayloadTransaction(); err != nil {
		return nil, err
	}
	if err := scope.validate(); err != nil {
		return nil, err
	}
	files, err := filestore.FromDB(s.db)
	if err != nil {
		return nil, err
	}
	sources, err := legacyQueueSources(s.db.WithContext(ctx), scope, false)
	if err != nil || len(sources) == 0 {
		return nil, err
	}
	if s.execution != nil {
		entities := queueExecutionEntities(scope, "")
		for _, source := range sources {
			entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntityLegacyPending, ID: source.ID})
		}
		if err := s.execution.Check(ctx, entities...); err != nil {
			if errors.Is(err, executionlineage.ErrQuarantined) {
				return nil, nil // Retain old sources for explicit review; never hydrate/replay them.
			}
			return nil, err
		}
	}
	rows := make([]models.ChatPendingTurn, 0, len(sources))
	prepared := make(map[string]filestore.Prepared, len(sources))
	for _, source := range sources {
		if source.ID == "" || len(source.ID) > 191 || strings.ContainsAny(source.ID, "\x00\r\n") {
			return nil, fmt.Errorf("legacy conversation receipt %s requires repair; source retained", source.ID)
		}
		body, err := files.Load(ctx, source.Payload, filestore.Bytes)
		if err != nil {
			return nil, err
		}
		publication, err := files.Prepare(ctx, bytes.NewReader(body), filestore.Bytes)
		if err != nil {
			return nil, err
		}
		prepared[source.ID] = publication
		rows = append(rows, models.ChatPendingTurn{ID: source.ID, Surface: source.Surface, SessionKey: source.SessionKey,
			Position: source.Position, Started: source.Started, Payload: body, CreatedAt: source.CreatedAt})
	}
	err = s.executionTransaction(ctx, scope, "", func(tx *gorm.DB) error {
		if s.execution != nil {
			for _, source := range sources {
				if err := s.execution.GuardContextTx(tx.Statement.Context, tx, executionlineage.Entity{Kind: executionlineage.EntityLegacyPending, ID: source.ID}); err != nil {
					return err
				}
			}
		}
		queue, err := s.lock(tx, scope)
		if err != nil {
			return err
		}
		current, err := legacyQueueSources(tx, scope, true)
		if err != nil {
			return err
		}
		if !sameLegacyQueueSources(sources, current) {
			return ErrQueueChanged
		}
		for _, row := range rows {
			identity := sha256.Sum256([]byte(row.ID))
			clientID := "legacy:" + hex.EncodeToString(identity[:])
			hash := sha256.Sum256(row.Payload)
			var previous models.ChatQueueTurn
			err := tx.Omit("payload").First(&previous, "id = ?", row.ID).Error
			if err == nil {
				raw, err := rawQueueBody(tx.Table("chat_queue_turns").Where("id = ? AND queue_id = ?", previous.ID, previous.QueueID))
				if err != nil {
					return err
				}
				if previous.ID != row.ID || previous.QueueID != queue.ID || previous.ActorID != actor || !sameLegacyQueueBody(raw, row.Payload) {
					return errors.New("legacy conversation receipt conflicts with existing data")
				}
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			queue.NextSequence++
			turn := models.ChatQueueTurn{ID: row.ID, QueueID: queue.ID, ActorID: actor, ClientMessageID: clientID,
				ClientKey:   clientKey(actor, clientID),
				Fingerprint: hex.EncodeToString(hash[:]), Payload: append([]byte(nil), row.Payload...),
				Sequence: queue.NextSequence, Position: queue.NextSequence, State: TurnQueued, CreatedAt: row.CreatedAt}
			if row.Started || len(row.Payload) == 0 {
				now := s.now()
				turn.State, turn.Reason, turn.FinishedAt = TurnUnknown, "legacy_interrupted", &now
			}
			if len(classify) > 0 {
				state, reason, err := classify[0](tx, row)
				if err != nil {
					return err
				}
				if state != TurnQueued && !terminalState(state) {
					return errors.New("invalid legacy conversation classification")
				}
				turn.State, turn.Reason, turn.FinishedAt = state, reason, nil
				if terminalState(state) {
					now := s.now()
					turn.FinishedAt = &now
				}
			}
			// A classifier may prove a terminal outcome from committed history,
			// but cannot grant permission to replay started or body-less work.
			if turn.State == TurnQueued && (row.Started || len(row.Payload) == 0) {
				now := s.now()
				turn.State, turn.Reason, turn.FinishedAt = TurnUnknown, "legacy_interrupted", &now
			}
			ref, err := files.PreparedRef(prepared[row.ID])
			if err != nil {
				return err
			}
			if err := s.insertQueueTurn(tx, &turn, ref); err != nil {
				return err
			}
			imported = append(imported, turn)
		}
		if err := tx.Model(queue).Update("next_sequence", queue.NextSequence).Error; err != nil {
			return err
		}
		return tx.Where("surface = ? AND session_key = ?", scope.Surface, scope.Key).Delete(&models.ChatPendingTurn{}).Error
	})
	return imported, err
}

type legacyQueueSource struct {
	ID         string
	Surface    string
	SessionKey string
	Position   int
	Started    bool
	Payload    string
	CreatedAt  time.Time
}

func legacyQueueSources(db *gorm.DB, scope QueueScope, lock bool) ([]legacyQueueSource, error) {
	var rows []legacyQueueSource
	query := db.Table("chat_pending_turns").Where("surface = ? AND session_key = ?", scope.Surface, scope.Key).Order("position, created_at, id")
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.Find(&rows).Error
	return rows, err
}

func sameLegacyQueueSources(before, after []legacyQueueSource) bool {
	if len(before) != len(after) {
		return false
	}
	for i, row := range before {
		other := after[i]
		if row.ID != other.ID || row.Surface != other.Surface || row.SessionKey != other.SessionKey || row.Position != other.Position || row.Started != other.Started || row.Payload != other.Payload || !row.CreatedAt.Equal(other.CreatedAt) {
			return false
		}
	}
	return true
}

func sameLegacyQueueBody(raw string, body []byte) bool {
	if filestore.IsRef(raw) {
		encoding, digest, err := filestore.Parse(raw)
		hash := sha256.Sum256(body)
		return err == nil && encoding == filestore.Bytes && digest == hex.EncodeToString(hash[:])
	}
	return !strings.HasPrefix(raw, filestore.Prefix) && bytes.Equal([]byte(raw), body)
}

func (s *DurableStore) Reopen(tx *gorm.DB, scope QueueScope) error {
	if err := s.guardExecutionTx(tx, scope, ""); err != nil {
		return err
	}
	queue, err := s.lock(tx, scope)
	if err != nil {
		return err
	}
	hasActive := queue.ActiveTurnID != ""
	if !hasActive {
		var count int64
		if err := tx.Model(&models.ChatQueueTurn{}).Where("queue_id = ? AND state = ?", queue.ID, TurnExecuting).Count(&count).Error; err != nil {
			return err
		}
		hasActive = count > 0
	}
	if hasActive {
		if queue.OwnerExpiresAt != nil && queue.OwnerExpiresAt.After(s.now()) {
			return ErrQueueChanged
		}
		if _, err := s.recoverActive(tx, queue); err != nil {
			return err
		}
	}
	return tx.Model(queue).Updates(map[string]any{"retired": false, "cancel_requested": false, "active_turn_id": "", "owner": "", "owner_expires_at": nil}).Error
}
