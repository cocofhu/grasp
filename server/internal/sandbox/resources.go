package sandbox

import (
	"sync/atomic"
	"time"
)

// agentIdleTimeout is the platform-wide Agent no-activity limit (settings page
// agent_idle_timeout_minutes); 0 until the settings service applies it.
var agentIdleTimeout atomic.Int64

// SetAgentIdleTimeout updates the no-activity limit sent with every later
// turn. A non-positive value clears it, so the boot value applies again.
func SetAgentIdleTimeout(d time.Duration) {
	agentIdleTimeout.Store(int64(max(d, 0)))
}

// AgentIdleTimeout returns the configured no-activity limit, or 0 when the
// settings service has not set one.
func AgentIdleTimeout() time.Duration { return time.Duration(agentIdleTimeout.Load()) }

// sharedRunEnabled is the platform switch for shared run sandboxes (settings
// page shared_run_sandbox_enabled). Off until the settings service enables it.
var sharedRunEnabled atomic.Bool

// SetSharedRunEnabled turns the shared run sandbox mode on or off for runs
// started afterwards.
func SetSharedRunEnabled(on bool) { sharedRunEnabled.Store(on) }

// SharedRunEnabled reports whether workflows may run in one shared sandbox.
func SharedRunEnabled() bool { return sharedRunEnabled.Load() }

// defaultMemoryMB is the platform-wide sandbox memory limit (settings page
// sandbox_memory_mb). It is process-global because both the runtime provider
// and the interactive sandbox service own their own Manager.
var defaultMemoryMB atomic.Int64

// SetDefaultMemoryMB updates the memory limit applied to sandboxes whose Spec
// leaves it unset. Non-positive values are ignored.
func SetDefaultMemoryMB(mb int) {
	if mb > 0 {
		defaultMemoryMB.Store(int64(mb))
	}
}

// DefaultMemoryMB returns the current platform sandbox memory limit, or 0
// when none has been configured (the gateway default applies).
func DefaultMemoryMB() int { return int(defaultMemoryMB.Load()) }

// withDefaultMemory fills MemoryMB from the platform default when the caller
// did not pick one. The input is never mutated.
func withDefaultMemory(r *GWResources) *GWResources {
	mb := defaultMemoryMB.Load()
	if mb <= 0 || (r != nil && r.MemoryMB > 0) {
		return r
	}
	out := GWResources{}
	if r != nil {
		out = *r
	}
	out.MemoryMB = mb
	return &out
}
