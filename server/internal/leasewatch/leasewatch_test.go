package leasewatch

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestMaintainTransientFailureKeepsWorkerAndAdvancesDeadline(t *testing.T) {
	lost := errors.New("owner lost")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	original := time.Now().Add(100 * time.Millisecond)
	var attempts atomic.Int32
	renewed := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- Maintain(ctx, original, 5*time.Millisecond, lost, func(context.Context) (time.Time, error) {
			if attempts.Add(1) == 1 {
				return time.Time{}, errors.New("temporary database failover")
			}
			select {
			case renewed <- struct{}{}:
			default:
			}
			return time.Now().Add(time.Second), nil
		})
	}()
	select {
	case <-renewed:
	case err := <-done:
		t.Fatalf("a transient error cancelled a valid lease: %v", err)
	case <-time.After(time.Second):
		t.Fatal("renewal was never retried")
	}
	select {
	case err := <-done:
		t.Fatalf("renewed lease ended at its old deadline: %v", err)
	case <-time.After(time.Until(original) + 20*time.Millisecond):
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("wrong shutdown cause: %v", err)
	}
}

func TestMaintainStopsAtKnownDeadlineDuringDatabaseOutage(t *testing.T) {
	lost := errors.New("owner lost")
	initial := time.Now().Add(100 * time.Millisecond)
	var attempts atomic.Int32
	err := Maintain(context.Background(), initial, 5*time.Millisecond, lost, func(context.Context) (time.Time, error) {
		attempts.Add(1)
		return time.Time{}, errors.New("database unavailable")
	})
	if !errors.Is(err, lost) || time.Now().Before(initial) || attempts.Load() < 2 || attempts.Load() > 10 {
		t.Fatalf("outage did not stay bounded by the committed expiry: attempts=%d err=%v", attempts.Load(), err)
	}
}

func TestMaintainRejectedFenceCancelsImmediately(t *testing.T) {
	lost := errors.New("owner lost")
	started := time.Now()
	err := Maintain(context.Background(), started.Add(time.Second), time.Millisecond, lost, func(context.Context) (time.Time, error) {
		return time.Time{}, lost
	})
	if !errors.Is(err, lost) || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("a rejected fence waited for expiry: %v", err)
	}
}

func TestMaintainBlockedRenewalCannotKeepWorkerPastExpiry(t *testing.T) {
	lost := errors.New("owner lost")
	release := make(chan struct{})
	queryStopped := make(chan struct{})
	defer close(release)
	deadline := time.Now().Add(60 * time.Millisecond)
	err := Maintain(context.Background(), deadline, time.Millisecond, lost, func(ctx context.Context) (time.Time, error) {
		<-ctx.Done()
		close(queryStopped)
		<-release // Simulate a driver that is slow to return after cancellation.
		return time.Now().Add(time.Second), nil
	})
	if !errors.Is(err, lost) || time.Now().Before(deadline) {
		t.Fatalf("blocked renewal escaped its deadline: %v", err)
	}
	select {
	case <-queryStopped:
	case <-time.After(time.Second):
		t.Fatal("database query was not cancelled at expiry")
	}
}

func TestMaintainCannotReviveExpiredOrUnadvancedLease(t *testing.T) {
	lost := errors.New("owner lost")
	for _, advance := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "late_success"}[advance], func(t *testing.T) {
			deadline := time.Now().Add(50 * time.Millisecond)
			err := Maintain(context.Background(), deadline, time.Millisecond, lost, func(context.Context) (time.Time, error) {
				if advance {
					time.Sleep(time.Until(deadline) + time.Millisecond)
					return time.Now().Add(time.Second), nil
				}
				return deadline, nil
			})
			if !errors.Is(err, lost) {
				t.Fatalf("invalid renewal revived execution: %v", err)
			}
		})
	}
	if err := Maintain(context.Background(), time.Now().Add(-time.Second), 0, lost, func(context.Context) (time.Time, error) {
		t.Fatal("expired lease attempted renewal")
		return time.Time{}, nil
	}); !errors.Is(err, lost) {
		t.Fatalf("already expired lease was accepted: %v", err)
	}
}
