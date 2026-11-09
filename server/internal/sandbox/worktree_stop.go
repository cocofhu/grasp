package sandbox

import (
	"context"
	"errors"
	"sync"
)

var ErrLocalStopUnconfirmed = errors.New("原本地运行进程停止尚未确认，工作区和关联已保留")

// callbackBackendStop adapts the existing trusted test starter. Its one owned
// stop callback can outlive a caller timeout, but retries never spawn it twice.
func callbackBackendStop(stop func()) localBackendStop {
	var once sync.Once
	done := make(chan struct{})
	return func(ctx context.Context) error {
		once.Do(func() {
			go func() {
				defer close(done)
				if stop != nil {
					stop()
				}
			}()
		})
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// StopLocal confirms exit only for the local Agent/runtime processes proved to
// belong to this original launch. It never Release/End/Destroy's its directory.
func (s *Sandbox) StopLocal(ctx context.Context) error {
	if !s.IsLocal() {
		return ErrLocalStopUnconfirmed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	e := s.local
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrLocalStopUnconfirmed
	}
	if e.backendStopped {
		e.mu.Unlock()
		return nil
	}
	e.stopRequested = true
	stop, generation := e.backendStop, e.backendGeneration
	pending := append([]localBackendStop(nil), e.pendingStops...)
	e.mu.Unlock()
	if stop == nil {
		return ErrLocalStopUnconfirmed
	}
	if err := stop(ctx); err != nil {
		return errors.Join(ErrLocalStopUnconfirmed, err)
	}
	for _, stop := range pending {
		if err := stop(ctx); err != nil {
			return errors.Join(ErrLocalStopUnconfirmed, err)
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || generation != e.backendGeneration {
		return ErrLocalStopUnconfirmed
	}
	e.backendStopped = true
	e.pendingStops = nil
	return nil
}

func (s *Sandbox) LocalStopState() (requested, confirmed bool) {
	if !s.IsLocal() {
		return false, false
	}
	s.local.mu.Lock()
	defer s.local.mu.Unlock()
	return s.local.stopRequested, s.local.backendStopped
}
