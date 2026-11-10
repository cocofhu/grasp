package runtime

import (
	"context"
	"errors"
	"time"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/leasewatch"
	"gorm.io/gorm"
)

var errExecutionAuthorityLost = errors.New("native operation execution authority lost")

type executionWatchKey struct{}
type executionWatchScope struct {
	provider  *acpProvider
	run, node string
	operation string
	state     uint
}

// executionContext owns only this invocation's context. In particular a
// revoked old operation must never call AbortRun/CancelVisitorTurn, which can
// target a newer operation that reused the same logical chat identity.
func (c *acpProvider) executionContext(ctx context.Context, req NodeReq) (context.Context, func(), error) {
	return c.executionContextWithTiming(ctx, req, 45*time.Second, 15*time.Second)
}

func (c *acpProvider) executionContextWithTiming(ctx context.Context, req NodeReq, ttl, interval time.Duration) (context.Context, func(), error) {
	if c.opts.CheckExecution == nil {
		return ctx, func() {}, nil
	}
	check, doneCheck := context.WithTimeout(ctx, ttl)
	err := c.opts.CheckExecution(check, req)
	doneCheck()
	if err != nil {
		return ctx, func() {}, err
	}
	scope := executionWatchScope{provider: c, run: req.RunID, node: req.NodeID, state: req.StateRunID, operation: req.Operation.ID}
	if previous, _ := ctx.Value(executionWatchKey{}).(executionWatchScope); previous == scope {
		return ctx, func() {}, nil // The outer operation already owns its monitor.
	}
	work, cancel := context.WithCancelCause(context.WithValue(ctx, executionWatchKey{}, scope))
	expiresAt := time.Now().Add(ttl)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		err := leasewatch.Maintain(work, expiresAt, interval, errExecutionAuthorityLost, func(check context.Context) (time.Time, error) {
			err := c.opts.CheckExecution(check, req)
			if executionRevoked(err) {
				return time.Time{}, errors.Join(errExecutionAuthorityLost, err)
			}
			if err != nil {
				return time.Time{}, err
			}
			return time.Now().Add(ttl), nil
		})
		cancel(err)
	}()
	return work, func() { cancel(context.Canceled); <-stopped }, nil
}

func executionRevoked(err error) bool {
	return errors.Is(err, executionlineage.ErrInactive) || errors.Is(err, executionlineage.ErrConflict) ||
		errors.Is(err, executionlineage.ErrUnbound) || errors.Is(err, executionlineage.ErrQuarantined) ||
		errors.Is(err, executionlineage.ErrUninitialized) || errors.Is(err, gorm.ErrRecordNotFound)
}
