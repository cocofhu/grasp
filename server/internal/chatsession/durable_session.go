package chatsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/leasewatch"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// DurableConfig keeps execution/protocol callbacks separate from queue storage.
// Readiness is passive: stopped environments keep accepted work without waking.
type DurableConfig[T any] struct {
	Config[T]
	Decode func(models.ChatQueueTurn) (T, error)
	// DecodeMetadata reconstructs a display-only value from an inline preview.
	// Native execution always uses Decode with the full immutable payload.
	DecodeMetadata func(models.ChatQueueTurn) (T, error)
	Intent         func(T) (QueueIntent, error)
	Admit          func(*gorm.DB) error
	Ready          func() (bool, error)
	Recover        func(models.ChatQueueTurn) error
	// PublishExecution must commit a frame before native execution can proceed.
	// Publish remains the notification hook for already accepted queue edits.
	PublishExecution func(context.Context, string, map[string]any) error
	// Settle handles failures before the surface could publish its result.
	// It must call FinishExecution in the same transaction as the notice.
	Settle func(context.Context, models.ChatQueueTurn, T, string, error) error
}

// DurableSession executes SQL-owned commands. An in-memory wakeup is only a
// scheduling hint; it never owns or replaces the accepted set of messages.
type DurableSession[T any] struct {
	store *DurableStore
	scope QueueScope
	cfg   DurableConfig[T]

	mu          sync.Mutex
	pumping     bool
	wakeAgain   bool
	cancelled   bool
	last        Snapshot
	cancelWatch *durableCancelWatch
}

type durableCancelWatch struct {
	turnID string
	wake   chan struct{}
}

func NewDurableSession[T any](store *DurableStore, scope QueueScope, cfg DurableConfig[T]) *DurableSession[T] {
	if cfg.Capacity <= 0 {
		cfg.Capacity = DefaultCapacity
	}
	return &DurableSession[T]{store: store, scope: scope, cfg: cfg}
}

func (s *DurableSession[T]) Enqueue(item T, admit Admit[T]) (int, error) {
	receipt, err := s.EnqueueReceipt(item, admit)
	return receipt.Waiting, err
}

func (s *DurableSession[T]) EnqueueReceipt(item T, admit Admit[T]) (QueueReceipt, error) {
	if admit != nil {
		view := s.View()
		if err := admit(view.Active, view.HasActive, view.Pending); err != nil {
			return QueueReceipt{}, err
		}
	}
	intent, err := s.cfg.Intent(item)
	if err != nil {
		return QueueReceipt{}, err
	}
	receipt, err := s.store.Admit(context.Background(), s.scope, intent, s.cfg.Capacity, s.cfg.Admit)
	if err != nil {
		return QueueReceipt{}, err
	}
	s.PublishQueueState()
	if !receipt.Duplicate || receipt.Turn.State == TurnQueued {
		s.Wake()
	}
	return receipt, nil
}

func (s *DurableSession[T]) Wake() {
	s.mu.Lock()
	if s.pumping {
		s.wakeAgain = true
		s.mu.Unlock()
		return
	}
	s.pumping = true
	s.mu.Unlock()
	go s.run()
}

func (s *DurableSession[T]) run() {
	defer func() {
		s.mu.Lock()
		again := s.wakeAgain
		s.wakeAgain, s.pumping = false, false
		s.mu.Unlock()
		if again {
			s.Wake()
		} else if s.cfg.OnIdle != nil {
			s.cfg.OnIdle()
		}
	}()
	if s.cfg.Ready != nil {
		ready, err := s.cfg.Ready()
		if err != nil || !ready {
			return
		}
	}
	lease, recovered, err := s.store.Acquire(context.Background(), s.scope, uuid.NewString())
	if err != nil {
		if !errors.Is(err, ErrQueueOwned) && !errors.Is(err, ErrQueueRetired) {
			log.Warn().Err(err).Str("scope", s.scope.ID()).Msg("conversation executor admission deferred")
		}
		return
	}
	// Renew only while this worker is executing. A process crash leaves the
	// durable lease and dispatch marker for conservative takeover.
	leaseCtx, lose := context.WithCancelCause(context.Background())
	renewed := make(chan struct{})
	defer func() {
		lose(context.Canceled)
		<-renewed
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.store.Abandon(ctx, lease)
	}()
	go func() {
		defer close(renewed)
		err := leasewatch.Maintain(leaseCtx, lease.ExpiresAt, max(s.store.leaseTTL/3, time.Millisecond), ErrQueueOwnerLost,
			func(ctx context.Context) (time.Time, error) { return s.store.RenewUntil(ctx, lease) })
		lose(err)
	}()
	if recovered != nil && terminalState(recovered.State) && s.cfg.Recover != nil {
		if err := s.cfg.Recover(*recovered); err != nil {
			log.Warn().Err(err).Str("turn", recovered.ID).Msg("interrupted conversation receipt retained; notice deferred")
			return
		}
	}
	for leaseCtx.Err() == nil {
		if s.cfg.Ready != nil {
			ready, err := s.cfg.Ready()
			if err != nil || !ready {
				return
			}
		}
		row, err := s.store.Claim(leaseCtx, lease)
		if err != nil || row == nil {
			if err != nil {
				log.Warn().Err(err).Str("scope", s.scope.ID()).Msg("conversation claim deferred")
			}
			return
		}
		item, decodeErr := s.cfg.Decode(*row)
		ctx, cancel := context.WithCancelCause(leaseCtx)
		ctx = executionContext(ctx, Execution{Store: s.store, Lease: lease, TurnID: row.ID})
		if decodeErr != nil {
			if err := s.settle(ctx, *row, item, TurnUnknown, fmt.Errorf("accepted message cannot be decoded: %w", decodeErr)); err != nil {
				cancel(err)
				return
			}
			cancel(context.Canceled)
			s.PublishQueueState()
			continue
		}
		watcher := &durableCancelWatch{turnID: row.ID, wake: make(chan struct{}, 1)}
		s.mu.Lock()
		s.cancelled = false
		s.cancelWatch = watcher
		s.mu.Unlock()
		stopped := make(chan struct{})
		watched := make(chan struct{})
		watchCtx, stopWatch := context.WithCancel(ctx)
		go func() { defer close(watched); s.watchCancel(watchCtx, lease, watcher, cancel, stopped) }()
		err = nil
		if s.cfg.BeforeTurn != nil {
			err = guardTurn(func() { s.cfg.BeforeTurn(item, ctx.Done()) })
		}
		if publishErr := s.publishExecution(ctx, EventQueueState, nil); err == nil {
			err = publishErr
		}
		begin := map[string]any{"item": s.cfg.View(item).Frame()}
		if s.cfg.TurnBeginExtra != nil {
			for key, value := range s.cfg.TurnBeginExtra(item) {
				begin[key] = value
			}
		}
		if publishErr := s.publishExecution(ctx, EventTurnBegin, begin); err == nil {
			err = publishErr
		}
		interrupted := false
		if err == nil {
			var executeErr error
			if err = guardTurn(func() { interrupted, executeErr = s.cfg.Execute(ctx, item) }); err == nil {
				err = executeErr
			}
		}
		close(stopped)
		stopWatch()
		<-watched // a late native Cancel must never affect the next turn
		s.mu.Lock()
		if s.cancelWatch == watcher {
			s.cancelWatch = nil
		}
		s.mu.Unlock()
		if s.cfg.AfterTurn != nil {
			if afterErr := guardTurn(func() { s.cfg.AfterTurn(item) }); err == nil {
				err = afterErr
			}
		}
		s.mu.Lock()
		wasCancel := s.cancelled || interrupted
		s.mu.Unlock()
		if errors.Is(err, ErrPreparationDeferred) {
			deferCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			deferErr := s.store.DeferPreparation(deferCtx, lease, row.ID)
			done()
			cancel(context.Canceled)
			if deferErr != nil {
				log.Warn().Err(deferErr).Str("turn", row.ID).Msg("conversation preparation remains recorded for recovery")
			}
			s.PublishQueueState()
			return
		}
		state := TurnSucceeded
		if err != nil {
			state = TurnFailed
		}
		if wasCancel {
			state = TurnCancelled
		}
		if finishErr := s.settle(ctx, *row, item, state, err); finishErr != nil {
			cancel(finishErr)
			log.Warn().Err(finishErr).Str("turn", row.ID).Msg("conversation result not committed; durable receipt retained")
			return
		}
		if receipt, receiptErr := s.store.Receipt(context.Background(), s.scope, row.ID); receiptErr == nil && receipt.State == TurnCancelled {
			wasCancel = true
		}
		cancel(context.Canceled)
		terminal := map[string]any{"item": s.cfg.View(item).Frame(), "interrupted": wasCancel}
		event := EventTurnDone
		if err != nil && !wasCancel {
			event, terminal["message"] = EventError, err.Error()
			if s.cfg.ErrorExtra != nil {
				for key, value := range s.cfg.ErrorExtra(err) {
					terminal[key] = value
				}
			}
		}
		if snapshot, snapErr := s.store.SnapshotMetadata(context.Background(), s.scope); snapErr == nil && snapshot.Queue.Generation == lease.Generation {
			if err := s.publishExecution(context.WithoutCancel(ctx), event, terminal); err != nil {
				log.Warn().Err(err).Str("turn", row.ID).Msg("conversation result saved; stream notification deferred")
			}
		}
	}
}

func (s *DurableSession[T]) settle(ctx context.Context, row models.ChatQueueTurn, item T, state string, failure error) error {
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	receipt, err := s.store.Receipt(settleCtx, s.scope, row.ID)
	if err != nil {
		return err
	}
	if terminalState(receipt.State) {
		return nil
	}
	if receipt.DispatchedAt != nil && failure != nil {
		state = TurnUnknown
	}
	if s.cfg.Settle != nil {
		return s.cfg.Settle(settleCtx, row, item, state, failure)
	}
	return s.store.db.WithContext(settleCtx).Transaction(func(tx *gorm.DB) error {
		reason := ""
		if failure != nil {
			reason = "execution_interrupted"
		}
		return FinishExecution(settleCtx, tx, state, reason, "")
	})
}

func (s *DurableSession[T]) watchCancel(ctx context.Context, lease QueueLease, watcher *durableCancelWatch, cancel context.CancelCauseFunc, done <-chan struct{}) {
	// Same-replica requests wake this watcher after their SQL commit. A low
	// frequency read remains the authority for other replicas and close/retire.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-watcher.wake:
		case <-ticker.C:
		}
		if ctx.Err() != nil {
			return
		}
		requested, err := s.store.Cancellation(ctx, lease, watcher.turnID)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			if !errors.Is(err, ErrQueueOwnerLost) && !errors.Is(err, ErrQueueChanged) {
				// A failed read says nothing about ownership. The independent
				// lease watcher still enforces the last acknowledged expiry.
				continue
			}
			if receipt, readErr := s.store.Receipt(ctx, s.scope, watcher.turnID); readErr == nil && terminalState(receipt.State) {
				return
			}
			cancel(ErrQueueOwnerLost)
			return
		}
		if requested {
			s.mu.Lock()
			current := s.cancelWatch == watcher
			if current {
				s.cancelled = true
			}
			s.mu.Unlock()
			if !current {
				return
			}
			cancel(context.Canceled)
			if s.cfg.CancelTurn != nil {
				s.cfg.CancelTurn()
			}
			return
		}
	}
}

func (s *DurableSession[T]) decodedSnapshot() (DurableSnapshot, []T, T, bool, error) {
	if s.cfg.DecodeMetadata != nil {
		return s.decodedMetadataSnapshot()
	}
	var zero T
	snapshot, err := s.store.Snapshot(context.Background(), s.scope)
	if err != nil {
		return snapshot, nil, zero, false, err
	}
	pending := make([]T, 0, len(snapshot.Pending))
	for _, row := range snapshot.Pending {
		item, err := s.cfg.Decode(row)
		if err != nil {
			return snapshot, nil, zero, false, err
		}
		pending = append(pending, item)
	}
	if snapshot.Active == nil {
		return snapshot, pending, zero, false, nil
	}
	active, err := s.cfg.Decode(*snapshot.Active)
	return snapshot, pending, active, true, err
}

func (s *DurableSession[T]) SnapshotChecked() (Snapshot, error) {
	snapshot, pending, active, hasActive, err := s.decodedSnapshot()
	if err != nil {
		return Snapshot{}, err
	}
	result := Snapshot{SessionGeneration: snapshot.Queue.Generation, Revision: snapshot.Queue.Revision,
		Waiting: len(pending), Busy: hasActive, Items: make([]map[string]any, 0, len(pending))}
	for _, item := range pending {
		result.Items = append(result.Items, s.cfg.View(item).Frame())
	}
	if hasActive {
		result.ActiveItem = s.cfg.View(active).Frame()
	}
	s.mu.Lock()
	s.last = result
	s.mu.Unlock()
	return result, nil
}

func (s *DurableSession[T]) Snapshot() Snapshot {
	if result, err := s.SnapshotChecked(); err == nil {
		return result
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// A failed read cannot declare accepted work gone or an executor idle.
	last := s.last
	last.Busy = true
	return last
}

func (s *DurableSession[T]) publish(event string, extra map[string]any) {
	payload, err := s.framePayload(extra)
	if err == nil && s.cfg.Publish != nil {
		s.cfg.Publish(event, payload)
	}
}

func (s *DurableSession[T]) publishExecution(ctx context.Context, event string, extra map[string]any) error {
	payload, err := s.framePayload(extra)
	if err != nil {
		return err
	}
	if s.cfg.PublishExecution != nil {
		return s.cfg.PublishExecution(ctx, event, payload)
	}
	if s.cfg.Publish != nil {
		s.cfg.Publish(event, payload)
	}
	return nil
}

func (s *DurableSession[T]) framePayload(extra map[string]any) (map[string]any, error) {
	snapshot, err := s.SnapshotChecked()
	if err != nil {
		return nil, err
	}
	payload := snapshot.QueueState()
	for key, value := range s.cfg.FrameExtra {
		payload[key] = value
	}
	for key, value := range extra {
		payload[key] = value
	}
	return payload, nil
}

func (s *DurableSession[T]) PublishQueueState() { s.publish(EventQueueState, nil) }

func (s *DurableSession[T]) CancelChecked(activeID string, clear bool) (bool, error) {
	s.mu.Lock()
	watcher := s.cancelWatch
	s.mu.Unlock()
	matched, err := s.store.Cancel(context.Background(), s.scope, activeID, clear)
	if err == nil && matched {
		s.mu.Lock()
		// A slow request must never wake a watcher belonging to a newer turn.
		if watcher != nil && s.cancelWatch == watcher && (activeID == "" || watcher.turnID == activeID) {
			select {
			case watcher.wake <- struct{}{}:
			default:
			}
		}
		s.mu.Unlock()
		s.PublishQueueState()
	}
	return matched, err
}

func (s *DurableSession[T]) Cancel(clear bool) {
	if _, err := s.CancelChecked("", clear); err != nil {
		log.Warn().Err(err).Str("scope", s.scope.ID()).Msg("conversation cancellation could not be committed")
	}
}

func (s *DurableSession[T]) CancelActive(id string, clear bool) bool {
	if id == "" {
		return false
	}
	matched, err := s.CancelChecked(id, clear)
	return err == nil && matched
}

func (s *DurableSession[T]) Remove(id string) error {
	if err := s.store.Remove(context.Background(), s.scope, id); err != nil {
		return err
	}
	s.PublishQueueState()
	return nil
}

func (s *DurableSession[T]) Reorder(ids []string) error {
	if err := s.store.Reorder(context.Background(), s.scope, ids); err != nil {
		return err
	}
	s.PublishQueueState()
	return nil
}

func (s *DurableSession[T]) Ready() bool {
	snapshot, err := s.SnapshotChecked()
	return err == nil && !snapshot.Busy && snapshot.Waiting == 0
}

func (s *DurableSession[T]) Idle() bool {
	s.mu.Lock()
	pumping := s.pumping
	s.mu.Unlock()
	return !pumping && s.Ready()
}

func (s *DurableSession[T]) Active() (T, bool) {
	_, _, active, hasActive, err := s.decodedSnapshot()
	return active, hasActive && err == nil
}

func (s *DurableSession[T]) View() View[T] {
	_, pending, active, hasActive, err := s.decodedSnapshot()
	if err != nil {
		return View[T]{Pumping: true}
	}
	s.mu.Lock()
	pumping := s.pumping
	s.mu.Unlock()
	return View[T]{Active: active, HasActive: hasActive, Pending: pending, Pumping: pumping}
}

func (s *DurableSession[T]) CancelRequested() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

func JSONDecoder[T any](view func(T) ItemView) func(models.ChatQueueTurn) (T, error) {
	return func(row models.ChatQueueTurn) (T, error) {
		var item T
		if err := json.Unmarshal(row.Payload, &item); err != nil {
			return item, err
		}
		if view(item).ID != row.ID {
			return item, errors.New("accepted message identity disagrees with its payload")
		}
		return item, nil
	}
}
