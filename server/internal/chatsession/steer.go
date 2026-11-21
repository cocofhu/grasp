package chatsession

import (
	"context"
	"errors"
	"strings"
)

// SteerResult is a runtime's answer to adding a pending item to its running
// turn (Config.SteerTurn).
type SteerResult int

const (
	// SteerRejected: the running turn cannot take the item (not supported,
	// or delivery failed). The turn is interrupted and the item runs next.
	SteerRejected SteerResult = iota
	// SteerJoined: the running turn took the item, which leaves the queue.
	SteerJoined
	// SteerDeferred: the item cannot reach the turn, which must not be
	// interrupted either: it already finished, or it has not reached its agent
	// yet (interrupting would drop it unseen). The item runs next.
	SteerDeferred
)

// SteerOutcome is what Steer did with the item.
type SteerOutcome string

const (
	// SteerOutcomeJoined: the item became part of the active turn.
	SteerOutcomeJoined SteerOutcome = "joined"
	// SteerOutcomeNext: the item moved to the head of the queue; an active
	// turn that could not take it was interrupted.
	SteerOutcomeNext SteerOutcome = "next"
)

// ErrItemActive is returned by Steer for the item that is already running.
var ErrItemActive = errors.New("item is already running")

// Steer handles the pending item id now instead of in queue order: the active
// turn's runtime takes it when it can (Config.SteerTurn); otherwise the item
// moves to the head of the queue and the active turn is interrupted, so the
// item runs as soon as that turn settles.
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
	outcome, frame, err := s.steerGated(ctx, id)
	if err != nil {
		return "", err
	}
	// Like cancel: a slow viewer must not hold the gate.
	if frame != nil {
		s.publish(*frame)
	}
	return outcome, nil
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
	item, active, hasActive := s.queue[idx], s.active, s.hasActive
	deliverCtx, stop := context.WithCancel(ctx)
	defer stop()
	s.steerStop = stop
	s.mu.Unlock()

	result := SteerRejected
	if hasActive && s.cfg.SteerTurn != nil {
		if err := guardTurn(func() { result = s.cfg.SteerTurn(deliverCtx, active, item) }); err != nil {
			result = SteerRejected
		}
	}

	s.mu.Lock()
	s.steerStop = nil
	if result == SteerRejected && deliverCtx.Err() != nil {
		result = SteerDeferred
	}
	// Remove does not take the gate; the item may have left meanwhile.
	if idx = s.indexLocked(id); idx < 0 {
		s.mu.Unlock()
		if result == SteerJoined {
			return SteerOutcomeJoined, nil, nil
		}
		return "", nil, ErrNotFound
	}
	s.queue = append(s.queue[:idx], s.queue[idx+1:]...)
	outcome := SteerOutcomeJoined
	if result != SteerJoined {
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
