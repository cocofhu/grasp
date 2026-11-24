package chatsession

import (
	"context"
	"errors"
	"strings"
	"time"
)

// SteerResult is a runtime's answer to adding a pending item to its running
// turn (Config.SteerTurn).
type SteerResult int

const (
	// SteerRejected: the running turn cannot take input at all (the agent
	// does not support it). The turn is interrupted and the item runs next.
	SteerRejected SteerResult = iota
	// SteerJoined: the running turn took the item, which leaves the queue.
	SteerJoined
	// SteerDeferred: the item cannot reach the turn, which must not be
	// interrupted either: it already finished, or it has not reached its agent
	// yet (interrupting would drop it unseen). The item runs next.
	SteerDeferred
	// SteerDeclined: the agent handed the item back, or delivery failed,
	// while the turn goes on. The turn is left to finish and the item runs
	// right after it.
	SteerDeclined
)

// SteerOutcome is what Steer did with the item.
type SteerOutcome string

const (
	// SteerOutcomeJoined: the item became part of the active turn.
	SteerOutcomeJoined SteerOutcome = "joined"
	// SteerOutcomeNext: the item moved to the head of the queue; an active
	// turn that could not take input at all was interrupted.
	SteerOutcomeNext SteerOutcome = "next"
	// SteerOutcomeQueued: the active turn did not take the item; it waits at
	// the head of the queue for that turn to finish.
	SteerOutcomeQueued SteerOutcome = "queued"
)

// Steer marks on a pending item, carried as its "steer" field in queue_state.
const (
	// SteerMarkPending: the item is being offered to the active turn.
	SteerMarkPending = "pending"
	// SteerMarkQueued: the active turn did not take the item; it runs once
	// that turn finishes.
	SteerMarkQueued = "queued"
)

// steerTurnEndGrace bounds a delivery once the active turn has ended. The
// runtime answers by the end of its turn; this covers an answer lost in
// transit and exceeds the sandbox client's own wait for it. A var so tests
// can shrink it.
var steerTurnEndGrace = 20 * time.Second

// ErrItemActive is returned by Steer for the item that is already running.
var ErrItemActive = errors.New("item is already running")

// Steer handles the pending item id now instead of in queue order: the active
// turn's runtime takes it when it can (Config.SteerTurn). Otherwise the item
// moves to the head of the queue: a turn that cannot take input at all is
// interrupted, so the item runs as soon as it settles; a turn that handed the
// item back finishes first.
//
// The runtime may take the item only at the turn's next step, so delivery
// lasts as long as the active turn does (plus steerTurnEndGrace). Meanwhile
// the item is marked SteerMarkPending in queue_state.
//
// The whole exchange holds the transition gate. The active turn therefore
// cannot be replaced meanwhile, and the pump cannot start the item as a turn
// of its own while it is being delivered into the current one. A cancel of
// that turn ends the delivery instead of waiting for it, and so does the end
// of ctx; unless the runtime already took the item, it then stays at the head
// of the queue without interrupting the turn, for a cancel to keep or drop.
func (s *Session[T]) Steer(ctx context.Context, id string) (SteerOutcome, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", errors.New("item id required")
	}
	if frame := s.markSteerPending(id); frame != nil {
		s.publish(*frame)
	}
	outcome, frame, err := s.steerGated(ctx, id)
	if frame == nil && err != nil {
		frame = s.clearSteerPending(id)
	}
	// Like cancel: a slow viewer must not hold the gate.
	if frame != nil {
		s.publish(*frame)
	}
	if err != nil {
		return "", err
	}
	return outcome, nil
}

// markSteerPending marks id as being offered to the active turn and returns
// the queue_state to publish, or nil when no delivery will be attempted.
func (s *Session[T]) markSteerPending(id string) *sessionFrame {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasActive || s.cfg.SteerTurn == nil || s.indexLocked(id) < 0 {
		return nil
	}
	if s.steerMarks == nil {
		s.steerMarks = map[string]string{}
	}
	s.steerMarks[id] = SteerMarkPending
	frame := s.queueFrameLocked(s.hasActive)
	return &frame
}

// clearSteerPending drops a pending mark Steer left behind when it failed.
func (s *Session[T]) clearSteerPending(id string) *sessionFrame {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.steerMarks[id] != SteerMarkPending {
		return nil
	}
	delete(s.steerMarks, id)
	frame := s.queueFrameLocked(s.hasActive)
	return &frame
}

// steerGated is Steer under the transition gate. The frame, when set, is
// published once the gate is released.
func (s *Session[T]) steerGated(ctx context.Context, id string) (SteerOutcome, *sessionFrame, error) {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()

	s.mu.Lock()
	if s.hasActive && s.cfg.View(s.active).ID == id {
		s.mu.Unlock()
		return "", nil, ErrItemActive
	}
	idx := s.indexLocked(id)
	if idx < 0 {
		s.mu.Unlock()
		return "", nil, ErrNotFound
	}
	item, active, hasActive, turn := s.queue[idx], s.active, s.hasActive, s.turnCtx
	// Once sent, input may land whatever the caller does; giving up on the
	// answer would run it a second time. Only cancel and the end of the turn
	// stop the delivery.
	deliverCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	defer stop()
	s.steerStop = stop
	s.mu.Unlock()

	result := SteerRejected
	if hasActive && s.cfg.SteerTurn != nil {
		if turn != nil {
			grace := steerTurnEndGrace
			defer context.AfterFunc(turn, func() { time.AfterFunc(grace, stop) })()
		}
		if err := guardTurn(func() { result = s.cfg.SteerTurn(deliverCtx, active, item) }); err != nil {
			result = SteerRejected
		}
	}

	s.mu.Lock()
	s.steerStop = nil
	if result != SteerJoined && deliverCtx.Err() != nil {
		result = SteerDeferred
	}
	delete(s.steerMarks, id)
	// Remove does not take the gate; the item may have left meanwhile.
	if idx = s.indexLocked(id); idx < 0 {
		frame := s.queueFrameLocked(s.hasActive)
		s.mu.Unlock()
		if result == SteerJoined {
			return SteerOutcomeJoined, &frame, nil
		}
		return "", &frame, ErrNotFound
	}
	s.queue = append(s.queue[:idx], s.queue[idx+1:]...)
	outcome := SteerOutcomeJoined
	switch result {
	case SteerJoined:
	case SteerDeclined:
		outcome = SteerOutcomeQueued
		s.queue = append([]T{item}, s.queue...)
		if s.hasActive {
			if s.steerMarks == nil {
				s.steerMarks = map[string]string{}
			}
			s.steerMarks[id] = SteerMarkQueued
		}
	default:
		outcome = SteerOutcomeNext
		s.queue = append([]T{item}, s.queue...)
	}
	interrupt := hasActive && result == SteerRejected
	var cancelFn context.CancelFunc
	if interrupt {
		s.cancelRequested = true
		cancelFn = s.cancelFn
	}
	frame := s.queueFrameLocked(s.hasActive)
	state := s.persistStateLocked()
	s.mu.Unlock()

	s.persist(state)
	if interrupt {
		if cancelFn != nil {
			cancelFn()
		}
		if s.cfg.CancelTurn != nil {
			s.cfg.CancelTurn()
		}
	}
	return outcome, &frame, nil
}

func (s *Session[T]) indexLocked(id string) int {
	for i, it := range s.queue {
		if s.cfg.View(it).ID == id {
			return i
		}
	}
	return -1
}
