package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/sandbox"
)

// ErrNodeBudget marks a node that used up its total time limit. It is not a
// sandbox fault, so it is never retried in a fresh sandbox.
var ErrNodeBudget = errors.New("node time budget exhausted")

// DefaultNodeHardCap is the platform's limit for a node whose config.timeout
// is empty, so a busy-looping Agent cannot hold a sandbox forever.
const DefaultNodeHardCap = 24 * time.Hour

// nodeBudgetError reports which limit a node ran into.
type nodeBudgetError struct {
	limit   time.Duration
	used    time.Duration
	hardCap bool
}

func (e *nodeBudgetError) Error() string {
	if e.hardCap {
		return fmt.Sprintf("节点运行超过平台兜底上限 %s(已用 %s)", fmtBudget(e.limit), fmtBudget(e.used))
	}
	return fmt.Sprintf("节点运行超过总时限 %s(已用 %s)", fmtBudget(e.limit), fmtBudget(e.used))
}

func (e *nodeBudgetError) Unwrap() error { return ErrNodeBudget }

func fmtBudget(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return fmt.Sprintf("%d 小时", int(d/time.Hour))
	}
	if d < time.Minute {
		return fmt.Sprintf("%d 秒", int(d/time.Second))
	}
	return fmt.Sprintf("%d 分钟", int((d+30*time.Second)/time.Minute))
}

// nodeBudgets accumulates the Agent turn time each runID|nodeID execution has
// spent. Only time inside a turn is charged, so waiting for a human reply or
// for a sandbox to be created never counts. In memory: a server restart
// starts the node's budget over.
type nodeBudgets struct {
	mu   sync.Mutex
	used map[string]time.Duration
}

func (b *nodeBudgets) get(key string) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used[key]
}

func (b *nodeBudgets) add(key string, d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used == nil {
		b.used = map[string]time.Duration{}
	}
	b.used[key] += d
}

func (b *nodeBudgets) reset(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.used, key)
}

// nodeLimit is the node's total time limit: config.timeout minutes, else the
// platform hard cap. hardCap reports which one applies.
func (c *acpProvider) nodeLimit(req NodeReq) (limit time.Duration, hardCap bool) {
	if v, ok := toInt(req.Config["timeout"]); ok && v > 0 {
		return time.Duration(v) * time.Minute, false
	}
	if c.opts.NodeHardCap > 0 {
		return c.opts.NodeHardCap, true
	}
	return DefaultNodeHardCap, true
}

// agentIdle is the no-activity limit for the next turn: the settings page
// value once applied, else the boot value.
func (c *acpProvider) agentIdle() time.Duration {
	if d := sandbox.AgentIdleTimeout(); d > 0 {
		return d
	}
	return c.opts.ChatIdleTimeout
}

// resetNodeBudget starts a new execution of the node with a full budget.
func (c *acpProvider) resetNodeBudget(req NodeReq) {
	c.budgets.reset(reactKey(req))
}

// turnCtx bounds one Agent turn by what is left of the node's budget. When
// nothing is left the returned ctx is already cancelled with a
// *nodeBudgetError cause, which streamChat reports without sending the turn.
func (c *acpProvider) turnCtx(ctx context.Context, req NodeReq) (context.Context, context.CancelFunc) {
	limit, hardCap := c.nodeLimit(req)
	used := c.budgets.get(reactKey(req))
	if used >= limit {
		cctx, cancel := context.WithCancelCause(ctx)
		cancel(&nodeBudgetError{limit: limit, used: used, hardCap: hardCap})
		return cctx, func() { cancel(nil) }
	}
	return context.WithDeadlineCause(ctx, time.Now().Add(limit-used),
		&nodeBudgetError{limit: limit, used: limit, hardCap: hardCap})
}

// budgetErr returns the node-budget cause behind ctx, or nil.
func budgetErr(ctx context.Context) error {
	if cause := context.Cause(ctx); errors.Is(cause, ErrNodeBudget) {
		return cause
	}
	return nil
}
