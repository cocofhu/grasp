// Package leasewatch keeps a worker within its last acknowledged lease even
// while the database is temporarily unavailable.
package leasewatch

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type renewal struct {
	expiresAt time.Time
	err       error
}

// Maintain retries transient renewal failures until the last committed expiry.
// A rejected ownership fence stops immediately. Each query gets that same hard
// deadline; a blocked query cannot keep the worker alive past its known lease.
// The caller must cancel its worker when Maintain returns an error.
func Maintain(ctx context.Context, expiresAt time.Time, interval time.Duration, lost error, renew func(context.Context) (time.Time, error)) error {
	if interval <= 0 {
		interval = time.Second
	}
	queryCtx, stopQuery := context.WithCancel(ctx)
	defer stopQuery()
	expiry := time.NewTimer(max(time.Until(expiresAt), 0))
	defer expiry.Stop()
	wakeup := time.NewTimer(interval)
	defer wakeup.Stop()
	results := make(chan renewal, 1)
	backoff := min(interval, 100*time.Millisecond)
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-expiry.C:
			return expired(lost, lastErr)
		case <-wakeup.C:
			if !expiresAt.After(time.Now()) {
				return expired(lost, lastErr)
			}
			deadlineCtx, cancel := context.WithDeadline(queryCtx, expiresAt)
			go func() {
				defer cancel()
				deadline, err := renew(deadlineCtx)
				results <- renewal{deadline, err}
			}()
		case result := <-results:
			// A reply arriving after the acknowledged deadline cannot revive work,
			// even when the database eventually reports a successful update.
			if !expiresAt.After(time.Now()) {
				return expired(lost, result.err)
			}
			if errors.Is(result.err, lost) {
				return result.err
			}
			if result.err == nil {
				if !result.expiresAt.After(expiresAt) {
					return expired(lost, errors.New("renewal did not advance the lease deadline"))
				}
				expiresAt = result.expiresAt
				expiry.Reset(time.Until(expiresAt))
				lastErr = nil
				backoff = min(interval, 100*time.Millisecond)
				wakeup.Reset(interval)
				continue
			}
			lastErr = result.err
			wakeup.Reset(backoff)
			backoff = min(backoff*2, time.Second)
		}
	}
}

func expired(lost, failure error) error {
	if failure == nil {
		return lost
	}
	return fmt.Errorf("%w: last renewal: %v", lost, failure)
}
