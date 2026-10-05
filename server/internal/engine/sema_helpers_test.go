package engine

import (
	"math"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestMaxConcurrentAndSema(t *testing.T) {
	e, _ := setupEngine(t)
	if e.MaxConcurrent() <= 0 {
		t.Fatal("MaxConcurrent")
	}
	e.SetMaxConcurrent(3)
	if e.MaxConcurrent() != 3 {
		t.Fatalf("after set: %d", e.MaxConcurrent())
	}
	e.SetAutoRetryMax(-1)
	if e.AutoRetryMax() != 0 {
		t.Fatal("auto retry clamp")
	}
	e.SetAutoRetryMax(2)
	if e.AutoRetryMax() != 2 {
		t.Fatal("auto retry set")
	}

	s := newSema(1)
	if !s.TryAcquire() {
		t.Fatal("try")
	}
	if s.TryAcquire() {
		t.Fatal("full")
	}
	s.Release()
	s.Acquire()
	s.Release()
	if s.Limit() != 1 {
		t.Fatalf("limit=%d", s.Limit())
	}
	s.SetLimit(2)
	if s.Limit() != 2 {
		t.Fatal("set limit")
	}
}

// TestSetAutoRetryMaxInt64Bounds covers CodeQL #7: negative→0, MaxInt32 and
// MaxInt32+1 are stored without MaxInt32 product clamping (atomic.Int64).
func TestSetAutoRetryMaxInt64Bounds(t *testing.T) {
	e := &Engine{}
	e.SetAutoRetryMax(-5)
	if e.AutoRetryMax() != 0 {
		t.Fatalf("negative: got %d", e.AutoRetryMax())
	}
	e.SetAutoRetryMax(math.MaxInt32)
	if e.AutoRetryMax() != math.MaxInt32 {
		t.Fatalf("MaxInt32: got %d", e.AutoRetryMax())
	}
	e.SetAutoRetryMax(math.MaxInt32 + 1)
	if e.AutoRetryMax() != math.MaxInt32+1 {
		t.Fatalf("MaxInt32+1: got %d want %d", e.AutoRetryMax(), math.MaxInt32+1)
	}
	e.SetAutoRetryMax(math.MaxInt)
	if e.AutoRetryMax() != math.MaxInt {
		t.Fatalf("MaxInt: got %d", e.AutoRetryMax())
	}
}

func TestDefaultAppPreviewHelpersRemoved(t *testing.T) {
	// Preview review agents create no Gate actions/form; helpers were retired with
	// the Gate shell. Keep a compile-time reminder that parseActions still works
	// for human_gate.
	acts := parseActions(nil)
	if len(acts) != 0 {
		t.Fatalf("empty actions: %+v", acts)
	}
	_ = models.GateField{}
}
