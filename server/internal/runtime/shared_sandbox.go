package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/cocofhu/grasp/internal/sandbox"
)

// errSharedSandboxIncompatible means the run's shared container was built for
// a different backend / image / ConfigHome / container env than this node
// needs. The caller falls back to a node-private container.
var errSharedSandboxIncompatible = errors.New("共享沙箱与本节点配置不兼容")

// sharedSandbox is intentionally kept in the provider rather than in the
// engine. It owns the container and the exclusive lease while each node owns
// its ACP client/session. This keeps node transcripts isolated while preserving
// one workspace for the complete run.
type sharedSandbox struct {
	runID       string
	sb          *sandbox.Sandbox
	home        string
	fingerprint string
	generation  int
	ready       chan struct{}
	lease       chan struct{}
	wake        chan struct{}
	finalized   bool
	// holder is the id of the lease currently occupying lease; 0 when free.
	holder uint64
}

// sharedLease is one node's hold on a run's shared sandbox. release is
// owner-checked, so a stale or repeated release cannot free another node's
// lease.
type sharedLease struct {
	*sharedSandbox
	id uint64
}

// sharedCreateResult is what a creator hands back to the coordinator. A
// positive generation overrides the coordinator's counter (restart Attach).
type sharedCreateResult struct {
	sb          *sandbox.Sandbox
	home        string
	fingerprint string
	generation  int
	// discard, when set, tears down a result the coordinator cannot use
	// (AbortRun finalized the run while create was in flight): it must
	// destroy the container and forget every record create left behind.
	discard func()
}

// sharedSandboxCoordinator serializes Agent node use of one run's workspace.
// The coordinator is shared by every backend provider in a ProviderRegistry;
// the compatibility fingerprint keeps a node of another backend or Agent
// configuration out of a container that was not built for it.
type sharedSandboxCoordinator struct {
	mu   sync.Mutex
	runs map[string]*sharedSandbox
	// generations survives replacement/finalization so a new container is
	// auditable as generation 2, 3, … instead of looking like a fresh run.
	generations map[string]int
	terminal    map[string]bool
	nextLease   uint64
}

func newSharedSandboxCoordinator() *sharedSandboxCoordinator {
	return &sharedSandboxCoordinator{runs: make(map[string]*sharedSandbox), generations: make(map[string]int), terminal: make(map[string]bool)}
}

// managed reports whether this process has opened a shared generation for
// runID. A durable binding of an unmanaged run was left by a previous process.
func (c *sharedSandboxCoordinator) managed(runID string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generations[runID] > 0
}

// acquire obtains the run's one exclusive workspace lease. The create
// callback is called exactly once for a run generation. Waiters block until the
// first creator has connected or failed, then reuse the resulting container
// when its fingerprint equals theirs; otherwise errSharedSandboxIncompatible is
// returned without waiting for the lease.
// sharedAcquireWaitHook, when set (tests only), runs when acquire starts
// waiting for another caller's create.
var sharedAcquireWaitHook func(runID string)

func (c *sharedSandboxCoordinator) acquire(ctx context.Context, runID, fingerprint string, create func() (sharedCreateResult, error)) (*sharedLease, error) {
	if c == nil || strings.TrimSpace(runID) == "" {
		return nil, errors.New("共享沙箱需要 run_id")
	}
	c.mu.Lock()
	if c.terminal[runID] {
		c.mu.Unlock()
		return nil, errors.New("运行已结束，共享沙箱不可用")
	}
	entry := c.runs[runID]
	creator := false
	if entry == nil {
		generation := c.generations[runID] + 1
		if generation < 1 {
			generation = 1
		}
		c.generations[runID] = generation
		entry = &sharedSandbox{runID: runID, generation: generation, ready: make(chan struct{}), lease: make(chan struct{}, 1), wake: make(chan struct{})}
		c.runs[runID] = entry
		creator = true
	}
	ready := entry.ready
	c.mu.Unlock()

	if creator {
		res, err := create()
		c.mu.Lock()
		// AbortRun may finalize while create() is in flight: the entry is
		// already detached with sb==nil, so the creator must tear down the
		// orphaned container and ConfigHome itself.
		orphaned := entry.finalized || c.terminal[runID] || c.runs[runID] != entry
		if err == nil && res.sb != nil && !orphaned {
			entry.sb, entry.home, entry.fingerprint = res.sb, res.home, res.fingerprint
			if res.generation > 0 {
				entry.generation = res.generation
				if res.generation > c.generations[runID] {
					c.generations[runID] = res.generation
				}
			}
		}
		close(entry.ready)
		if err != nil || res.sb == nil || orphaned {
			if c.runs[runID] == entry {
				delete(c.runs, runID)
			}
		}
		c.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if res.sb == nil {
			return nil, errors.New("共享沙箱创建返回空容器")
		}
		if orphaned {
			if res.discard != nil {
				res.discard()
			} else {
				res.sb.Destroy(context.Background())
				removeHome(res.home)
			}
			return nil, errors.New("运行已结束，共享沙箱不可用")
		}
	} else {
		if sharedAcquireWaitHook != nil {
			sharedAcquireWaitHook(runID)
		}
		select {
		case <-ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		c.mu.Lock()
		ok := entry.sb != nil && !entry.finalized
		terminal := entry.finalized || c.terminal[runID]
		c.mu.Unlock()
		if !ok {
			if terminal {
				return nil, errors.New("运行已结束，共享沙箱不可用")
			}
			// The creator's create() failed (its node gets that error).
			// The entry is gone, so this waiter becomes the next creator
			// instead of failing with an error that is not its own.
			return c.acquire(ctx, runID, fingerprint, create)
		}
	}
	c.mu.Lock()
	compatible := entry.fingerprint == fingerprint
	c.mu.Unlock()
	if !compatible {
		return nil, errSharedSandboxIncompatible
	}
	select {
	case entry.lease <- struct{}{}:
		c.mu.Lock()
		valid := c.runs[runID] == entry && !entry.finalized
		terminal := c.terminal[runID]
		var id uint64
		if valid {
			c.nextLease++
			id = c.nextLease
			entry.holder = id
		}
		c.mu.Unlock()
		if valid {
			return &sharedLease{sharedSandbox: entry, id: id}, nil
		}
		if terminal {
			return nil, errors.New("运行已结束，共享沙箱不可用")
		}
		return c.acquire(ctx, runID, fingerprint, create)
	case <-entry.wake:
		c.mu.Lock()
		terminal := c.terminal[runID]
		c.mu.Unlock()
		if terminal {
			return nil, errors.New("运行已结束，共享沙箱不可用")
		}
		return c.acquire(ctx, runID, fingerprint, create)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// release frees l's lease. It is a no-op unless l is the current holder.
func (c *sharedSandboxCoordinator) release(l *sharedLease) {
	if c == nil || l == nil || l.sharedSandbox == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := l.sharedSandbox
	if entry.holder != l.id {
		return
	}
	entry.holder = 0
	select {
	case <-entry.lease:
	default:
	}
}

// discard removes a broken generation and returns its container/home for
// teardown. The caller owns Destroy and filesystem cleanup outside the lock.
func (c *sharedSandboxCoordinator) discard(runID string, sb *sandbox.Sandbox) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.runs[runID]
	if entry == nil || (sb != nil && entry.sb != sb) {
		return "", false
	}
	entry.finalized = true
	entry.holder = 0
	delete(c.runs, runID)
	close(entry.wake)
	select {
	case <-entry.lease:
	default:
	}
	return entry.home, true
}

// currentGeneration is the generation of the run's entry being created.
func (c *sharedSandboxCoordinator) currentGeneration(runID string) int {
	if c == nil {
		return 1
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return max(c.generations[runID], 1)
}

func (c *sharedSandboxCoordinator) nextGeneration(runID string) int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.generations[runID] + 1
}

// reopen starts a fresh lifecycle after an operator resumes a failed or
// cancelled run. It is called at the beginning of a new node execution.
func (c *sharedSandboxCoordinator) reopen(runID string) {
	if c == nil || strings.TrimSpace(runID) == "" {
		return
	}
	c.mu.Lock()
	if c.runs[runID] == nil {
		delete(c.terminal, runID)
	}
	c.mu.Unlock()
}

// finalize atomically detaches a run's current generation. It is idempotent;
// repeated AbortRun/finish calls cannot destroy the same container twice.
func (c *sharedSandboxCoordinator) finalize(runID string) (*sharedSandbox, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.terminal[runID] = true
	entry := c.runs[runID]
	if entry == nil || entry.finalized {
		return nil, false
	}
	entry.finalized = true
	entry.holder = 0
	delete(c.runs, runID)
	close(entry.wake)
	select {
	case <-entry.lease:
	default:
	}
	return entry, true
}

// nodeLease ties a node's ACP client to its shared lease. The record outlives
// an early release (KeepAliveForReview parking) so node teardown still knows
// the container belongs to the run and must not be retired or destroyed.
type nodeLease struct {
	lease *sharedLease
	once  sync.Once
}

func (n *nodeLease) release(c *sharedSandboxCoordinator) {
	if n == nil {
		return
	}
	n.once.Do(func() { c.release(n.lease) })
}

func (c *acpProvider) trackNodeLease(acp *sandbox.ACPClient, l *sharedLease) {
	if acp != nil && l != nil {
		c.nodeLeases.Store(acp, &nodeLease{lease: l})
	}
}

func (c *acpProvider) nodeLeaseFor(acp *sandbox.ACPClient) *nodeLease {
	if acp == nil {
		return nil
	}
	v, ok := c.nodeLeases.Load(acp)
	if !ok {
		return nil
	}
	return v.(*nodeLease)
}

func (c *acpProvider) takeNodeLease(acp *sandbox.ACPClient) *nodeLease {
	if acp == nil {
		return nil
	}
	v, ok := c.nodeLeases.LoadAndDelete(acp)
	if !ok {
		return nil
	}
	return v.(*nodeLease)
}

func safeSessionPart(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}
