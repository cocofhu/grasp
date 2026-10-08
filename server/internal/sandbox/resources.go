package sandbox

import "sync/atomic"

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
