package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/google/uuid"
)

// SharedSandboxStatus describes the durable lifecycle states used by the
// run-level coordinator and the sandbox registry.
type SharedSandboxStatus string

const (
	SharedSandboxCreating  SharedSandboxStatus = "creating"
	SharedSandboxRunning   SharedSandboxStatus = "running"
	SharedSandboxReplacing SharedSandboxStatus = "replacing"
	SharedSandboxRetired   SharedSandboxStatus = "retired"
	SharedSandboxFailed    SharedSandboxStatus = "failed"
)

// sharedSandbox is intentionally kept in the provider rather than in the
// engine. It owns the container and the exclusive lease while each node owns
// its ACP client/session. This keeps node transcripts isolated while preserving
// one workspace for the complete run.
type sharedSandbox struct {
	runID       string
	sb          *sandbox.Sandbox
	home        string
	bridgeModel string
	generation  int
	// envFingerprint identifies the node-level environment used to start the
	// ACP subprocess. The value is a digest, never the environment itself.
	// A run sandbox cannot silently hand one node's credentials to another.
	envFingerprint string
	ready          chan struct{}
	lease          chan struct{}
	wake           chan struct{}
	finalized      bool
}

// sharedSandboxCoordinator serializes Agent node use of one run's workspace.
// The coordinator is shared by every backend provider in a ProviderRegistry,
// so switching ACP backends cannot accidentally allocate a second container.
type sharedSandboxCoordinator struct {
	mu   sync.Mutex
	runs map[string]*sharedSandbox
	// generations survives replacement/finalization so a new container is
	// auditable as generation 2, 3, … instead of looking like a fresh run.
	generations map[string]int
	terminal    map[string]bool
}

func newSharedSandboxCoordinator() *sharedSandboxCoordinator {
	return &sharedSandboxCoordinator{runs: make(map[string]*sharedSandbox), generations: make(map[string]int), terminal: make(map[string]bool)}
}

// acquire obtains the run's one exclusive workspace lease. The create
// callback is called exactly once for a run generation. Waiters block until the
// first creator has connected or failed, then reuse the resulting container.
func (c *sharedSandboxCoordinator) acquire(ctx context.Context, runID string, create func() (*sandbox.Sandbox, string, string, error)) (*sharedSandbox, error) {
	return c.acquireWithFingerprint(ctx, runID, "", create)
}

// acquireWithFingerprint is the run-scoped entry point used by workflow
// providers. ACP bridge sessions inherit the container environment, so a
// node whose environment differs from the one that created the container must
// fail closed until the bridge supports safe per-session environment injection.
func (c *sharedSandboxCoordinator) acquireWithFingerprint(ctx context.Context, runID, envFingerprint string, create func() (*sandbox.Sandbox, string, string, error)) (*sharedSandbox, error) {
	if c == nil || strings.TrimSpace(runID) == "" {
		return nil, errors.New("共享沙箱需要 run_id")
	}
	envFingerprint = strings.TrimSpace(envFingerprint)
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
		entry.envFingerprint = envFingerprint
		c.runs[runID] = entry
		creator = true
	} else if entry.envFingerprint != "" && envFingerprint != "" && entry.envFingerprint != envFingerprint {
		c.mu.Unlock()
		return nil, errors.New("共享沙箱不支持节点级环境切换：为避免泄露前一节点凭据，已拒绝复用")
	}
	ready := entry.ready
	c.mu.Unlock()

	if creator {
		sb, home, bridgeModel, err := create()
		c.mu.Lock()
		if err == nil && sb != nil {
			entry.sb, entry.home, entry.bridgeModel = sb, home, bridgeModel
		}
		close(entry.ready)
		if err != nil || sb == nil {
			delete(c.runs, runID)
		}
		c.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if sb == nil {
			return nil, errors.New("共享沙箱创建返回空容器")
		}
	} else {
		select {
		case <-ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		c.mu.Lock()
		ok := entry.sb != nil && !entry.finalized
		c.mu.Unlock()
		if !ok {
			return nil, errors.New("共享沙箱不可用")
		}
	}
	select {
	case entry.lease <- struct{}{}:
		c.mu.Lock()
		valid := c.runs[runID] == entry && !entry.finalized
		terminal := c.terminal[runID]
		c.mu.Unlock()
		if valid {
			return entry, nil
		}
		if terminal {
			return nil, errors.New("运行已结束，共享沙箱不可用")
		}
		return c.acquireWithFingerprint(ctx, runID, envFingerprint, create)
	case <-entry.wake:
		c.mu.Lock()
		terminal := c.terminal[runID]
		c.mu.Unlock()
		if terminal {
			return nil, errors.New("运行已结束，共享沙箱不可用")
		}
		return c.acquireWithFingerprint(ctx, runID, envFingerprint, create)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *sharedSandboxCoordinator) release(sb *sandbox.Sandbox) {
	if c == nil || sb == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, entry := range c.runs {
		if entry.sb == sb {
			select {
			case <-entry.lease:
			default:
			}
			return
		}
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
	delete(c.runs, runID)
	close(entry.wake)
	select {
	case <-entry.lease:
	default:
	}
	return entry.home, true
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
// cancelled run. It is called at the beginning of a new node execution; late
// calls from the cancelled generation cannot clear terminal state themselves.
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
	entry := c.runs[runID]
	if entry == nil || entry.finalized {
		return nil, false
	}
	entry.finalized = true
	delete(c.runs, runID)
	c.terminal[runID] = true
	close(entry.wake)
	select {
	case <-entry.lease:
	default:
	}
	return entry, true
}

func newNodeChatID(runID, nodeID string) string {
	return fmt.Sprintf("run-%s-node-%s-%s", safeSessionPart(runID), safeSessionPart(nodeID), uuid.NewString()[:8])
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
