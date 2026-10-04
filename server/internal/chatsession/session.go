// Package chatsession is the platform-authoritative controller shared by agent
// chat surfaces: one FIFO of pending turns, a single pump that runs them in
// order on a background context, Cancel, and the queue_state / turn_begin /
// turn_done / error control frames that let any client rebuild the dialogue
// after a reconnect. Surfaces plug in how a turn executes, how it is persisted
// and where frames go; a session never fails a turn because a viewer left.
package chatsession

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/cocofhu/grasp/internal/models"
)

// DefaultCapacity caps pending (not-yet-started) turns. Aligned with the
// sandbox-gateway bridge PromptQueue (MaxPromptQueueItems=32).
const DefaultCapacity = 32

// Frame events published by a Session.
const (
	EventQueueState = "queue_state"
	EventTurnBegin  = "turn_begin"
	EventTurnDone   = "turn_done"
	EventError      = "error"
)

// ItemView is the client-visible part of a queued turn.
type ItemView struct {
	ID          string
	Text        string
	Images      []models.PromptImage
	Annotations []models.ReactAnnotation
}

// Frame renders the view in the queue_state / turn_begin item shape. Nil
// slices become empty so clients never drop attachment or annotation chips.
func (v ItemView) Frame() map[string]any {
	images := v.Images
	if images == nil {
		images = []models.PromptImage{}
	}
	annotations := v.Annotations
	if annotations == nil {
		annotations = []models.ReactAnnotation{}
	}
	return map[string]any{
		"id":          v.ID,
		"text":        v.Text,
		"images":      images,
		"annotations": annotations,
	}
}

// Config wires a Session to its surface. View, Execute and Publish are required.
type Config[T any] struct {
	// Capacity caps pending items; 0 means DefaultCapacity.
	Capacity int
	// FullError is returned when the queue is at capacity.
	FullError string
	// View exposes the client-visible fields of an item.
	View func(T) ItemView
	// Execute runs one turn. ctx is cancelled by Cancel; interrupted reports a
	// turn that stopped early on its own (e.g. agent idle timeout).
	Execute func(ctx context.Context, item T) (interrupted bool, err error)
	// Publish emits a control frame; FrameExtra is already merged.
	Publish func(event string, payload map[string]any)
	// FrameExtra is merged into every queue_state and turn_begin (e.g. kind).
	FrameExtra map[string]any
	// TurnBeginExtra adds per-item fields to turn_begin.
	TurnBeginExtra func(T) map[string]any
	// BeforeTurn runs when an item becomes active, before turn_begin; done
	// closes when the turn ends.
	BeforeTurn func(item T, done <-chan struct{})
	// AfterTurn runs once Execute has returned, before turn_done / error.
	AfterTurn func(item T)
	// OnDropped receives pending items removed by Cancel(clearQueue=true).
	OnDropped func([]T)
	// OnIdle runs after the pump exits with nothing active or pending.
	OnIdle func()
	// CancelTurn asks the runtime to stop its turn (e.g. ACP cancel). Cancel
	// calls it even with no active item, to stop a turn started elsewhere.
	CancelTurn func()
}

// Session is one chat's FIFO and pump. Use New.
type Session[T any] struct {
	cfg Config[T]

	mu        sync.Mutex
	queue     []T
	active    T
	hasActive bool
	cancelFn  context.CancelFunc
	pumping   bool
	// cancelRequested is set by Cancel; the active turn saves partial output
	// as interrupted when it returns.
	cancelRequested bool
	// liveEvents is the in-flight stream kept for reconnect seeding by
	// surfaces whose runtime does not hold one.
	liveEvents []models.AcpEvent
}

// New builds a Session from cfg.
func New[T any](cfg Config[T]) *Session[T] {
	if cfg.Capacity <= 0 {
		cfg.Capacity = DefaultCapacity
	}
	if cfg.FullError == "" {
		cfg.FullError = "消息队列已满，请稍候"
	}
	return &Session[T]{cfg: cfg}
}

// Snapshot is the refresh-resume view of a session.
type Snapshot struct {
	Waiting    int
	Busy       bool
	Items      []map[string]any
	ActiveItem map[string]any
}

// QueueState renders the snapshot as a queue_state payload.
func (sn Snapshot) QueueState() map[string]any {
	p := map[string]any{"waiting": sn.Waiting, "items": sn.Items, "busy": sn.Busy}
	if sn.ActiveItem != nil {
		p["activeItem"] = sn.ActiveItem
	}
	return p
}

// Admit inspects the locked queue before an item is added; a non-nil error
// rejects the item.
type Admit[T any] func(active T, hasActive bool, pending []T) error

// ErrNotFound is returned by Remove / Reorder for an unknown item id.
var ErrNotFound = errors.New("item not found")

// Enqueue appends item, publishes queue_state and starts the pump when idle.
// It returns the pending count including item.
func (s *Session[T]) Enqueue(item T, admit Admit[T]) (int, error) {
	s.mu.Lock()
	if admit != nil {
		if err := admit(s.active, s.hasActive, s.queue); err != nil {
			s.mu.Unlock()
			return 0, err
		}
	}
	if len(s.queue) >= s.cfg.Capacity {
		s.mu.Unlock()
		return 0, errors.New(s.cfg.FullError)
	}
	s.queue = append(s.queue, item)
	waiting := len(s.queue)
	startPump := !s.pumping
	if startPump {
		s.pumping = true
	}
	items := s.itemsLocked()
	s.mu.Unlock()

	s.publishQueue(waiting, items, true)
	if startPump {
		go s.pump()
	}
	return waiting, nil
}

// Remove drops one pending (not active) item.
func (s *Session[T]) Remove(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("item id required")
	}
	s.mu.Lock()
	if s.hasActive && s.cfg.View(s.active).ID == id {
		s.mu.Unlock()
		return errors.New("cannot remove active item")
	}
	idx := -1
	for i, it := range s.queue {
		if s.cfg.View(it).ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return ErrNotFound
	}
	s.queue = append(s.queue[:idx], s.queue[idx+1:]...)
	waiting, items, busy := len(s.queue), s.itemsLocked(), s.hasActive
	s.mu.Unlock()
	s.publishQueue(waiting, items, busy)
	return nil
}

// Reorder sets the pending order; ids must name every pending item once.
func (s *Session[T]) Reorder(ids []string) error {
	if len(ids) == 0 {
		return errors.New("item ids required")
	}
	s.mu.Lock()
	if len(s.queue) != len(ids) {
		s.mu.Unlock()
		return errors.New("item count mismatch")
	}
	byID := make(map[string]T, len(s.queue))
	for _, it := range s.queue {
		byID[s.cfg.View(it).ID] = it
	}
	reordered := make([]T, 0, len(ids))
	for _, id := range ids {
		it, ok := byID[strings.TrimSpace(id)]
		if !ok {
			s.mu.Unlock()
			return ErrNotFound
		}
		reordered = append(reordered, it)
	}
	s.queue = reordered
	waiting, items, busy := len(s.queue), s.itemsLocked(), s.hasActive
	s.mu.Unlock()
	s.publishQueue(waiting, items, busy)
	return nil
}

// Cancel stops the active turn. With clearQueue the pending items are dropped
// too; otherwise the pump moves on to the next one.
func (s *Session[T]) Cancel(clearQueue bool) {
	s.mu.Lock()
	var dropped []T
	if clearQueue {
		dropped = s.queue
		s.queue = nil
	}
	s.cancelRequested = true
	waiting, items, busy := len(s.queue), s.itemsLocked(), s.hasActive
	cancelFn := s.cancelFn
	s.mu.Unlock()

	if len(dropped) > 0 && s.cfg.OnDropped != nil {
		s.cfg.OnDropped(dropped)
	}
	s.publishQueue(waiting, items, busy)
	if s.cfg.CancelTurn != nil {
		s.cfg.CancelTurn()
	}
	if cancelFn != nil {
		cancelFn()
	}
}

// Snapshot returns the current queue view.
func (s *Session[T]) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	sn := Snapshot{Waiting: len(s.queue), Busy: s.hasActive, Items: s.itemsLocked()}
	if s.hasActive {
		sn.ActiveItem = s.cfg.View(s.active).Frame()
	}
	return sn
}

// Ready reports no active turn and no pending items.
func (s *Session[T]) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.hasActive && len(s.queue) == 0
}

// Idle reports Ready with no pump running; an idle session may be discarded.
func (s *Session[T]) Idle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.hasActive && len(s.queue) == 0 && !s.pumping
}

// Active returns the in-flight item.
func (s *Session[T]) Active() (T, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active, s.hasActive
}

// CancelRequested reports whether Cancel was called during the active turn.
func (s *Session[T]) CancelRequested() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelRequested
}

// SetLiveEvents records the active turn's stream so far.
func (s *Session[T]) SetLiveEvents(events []models.AcpEvent) {
	s.mu.Lock()
	s.liveEvents = events
	s.mu.Unlock()
}

// LiveEvents returns the active turn's recorded stream (nil when idle).
func (s *Session[T]) LiveEvents() []models.AcpEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasActive {
		return nil
	}
	return append([]models.AcpEvent(nil), s.liveEvents...)
}

// PublishQueueState re-emits the current queue_state, e.g. for a new viewer.
func (s *Session[T]) PublishQueueState() {
	s.publish(EventQueueState, s.Snapshot().QueueState())
}

func (s *Session[T]) itemsLocked() []map[string]any {
	out := make([]map[string]any, 0, len(s.queue))
	for _, it := range s.queue {
		out = append(out, s.cfg.View(it).Frame())
	}
	return out
}

func (s *Session[T]) publishQueue(waiting int, items []map[string]any, busy bool) {
	s.publish(EventQueueState, map[string]any{"waiting": waiting, "items": items, "busy": busy})
}

func (s *Session[T]) publish(event string, payload map[string]any) {
	if event == EventQueueState || event == EventTurnBegin {
		for k, v := range s.cfg.FrameExtra {
			if _, set := payload[k]; !set {
				payload[k] = v
			}
		}
	}
	s.cfg.Publish(event, payload)
}

func (s *Session[T]) pump() {
	defer s.pumpExit()
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			s.mu.Unlock()
			return
		}
		item := s.queue[0]
		s.queue = s.queue[1:]
		waiting := len(s.queue)
		s.active, s.hasActive = item, true
		s.cancelRequested = false
		s.liveEvents = nil
		ctx, cancel := context.WithCancel(context.Background())
		s.cancelFn = cancel
		items := s.itemsLocked()
		s.mu.Unlock()

		if s.cfg.BeforeTurn != nil {
			s.cfg.BeforeTurn(item, ctx.Done())
		}
		s.publishQueue(waiting, items, true)
		begin := map[string]any{"item": s.cfg.View(item).Frame()}
		if s.cfg.TurnBeginExtra != nil {
			for k, v := range s.cfg.TurnBeginExtra(item) {
				begin[k] = v
			}
		}
		s.publish(EventTurnBegin, begin)

		interrupted, err := s.cfg.Execute(ctx, item)

		cancel()
		if s.cfg.AfterTurn != nil {
			s.cfg.AfterTurn(item)
		}
		s.mu.Lock()
		var zero T
		s.active, s.hasActive = zero, false
		s.cancelFn = nil
		s.liveEvents = nil
		wasCancel := s.cancelRequested || interrupted
		s.cancelRequested = false
		s.mu.Unlock()

		if err != nil && !wasCancel {
			s.publish(EventError, map[string]any{"message": err.Error(), "interrupted": false})
		} else {
			s.publish(EventTurnDone, map[string]any{"interrupted": wasCancel})
		}
	}
}

func (s *Session[T]) pumpExit() {
	s.mu.Lock()
	restart := s.hasActive || len(s.queue) > 0
	s.pumping = restart
	s.mu.Unlock()
	if restart {
		// Items arrived while the last turn was finishing.
		go s.pump()
		return
	}
	if s.cfg.OnIdle != nil {
		s.cfg.OnIdle()
	}
}

// SeedForTest installs pending items without starting the pump.
func (s *Session[T]) SeedForTest(pending []T) {
	s.mu.Lock()
	s.queue = append([]T(nil), pending...)
	s.mu.Unlock()
}
