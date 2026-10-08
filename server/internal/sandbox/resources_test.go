package sandbox

import "testing"

func TestWithDefaultMemory(t *testing.T) {
	prev := defaultMemoryMB.Load()
	t.Cleanup(func() { defaultMemoryMB.Store(prev) })

	defaultMemoryMB.Store(0)
	if got := withDefaultMemory(nil); got != nil {
		t.Fatalf("no default configured: got %+v, want nil", got)
	}

	SetDefaultMemoryMB(8192)
	SetDefaultMemoryMB(0)
	SetDefaultMemoryMB(-1)
	if DefaultMemoryMB() != 8192 {
		t.Fatalf("non-positive values must be ignored, got %d", DefaultMemoryMB())
	}

	if got := withDefaultMemory(nil); got == nil || got.MemoryMB != 8192 {
		t.Fatalf("nil spec resources: got %+v", got)
	}

	in := &GWResources{CPUCores: 2, DiskGi: 40}
	got := withDefaultMemory(in)
	if got.MemoryMB != 8192 || got.CPUCores != 2 || got.DiskGi != 40 {
		t.Fatalf("partial resources: got %+v", got)
	}
	if in.MemoryMB != 0 {
		t.Fatalf("input mutated: %+v", in)
	}

	explicit := &GWResources{MemoryMB: 2048}
	if got := withDefaultMemory(explicit); got != explicit {
		t.Fatalf("explicit memory must win: got %+v", got)
	}
}
