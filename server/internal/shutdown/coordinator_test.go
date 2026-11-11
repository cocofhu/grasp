package shutdown

import (
	"testing"
	"time"
)

func TestCoordinatorGraceRemaining(t *testing.T) {
	c := New(600 * time.Second)
	if c.IsDraining() {
		t.Fatal("expected not draining initially")
	}
	c.BeginDraining()
	if !c.IsDraining() {
		t.Fatal("expected draining after BeginDraining")
	}
	rem := c.GraceRemainingSeconds()
	if rem < 598 || rem > 600 {
		t.Fatalf("grace remaining = %d, want ~600", rem)
	}
	if c.gracePeriod() != 600*time.Second {
		t.Fatalf("grace period = %v", c.gracePeriod())
	}
}

func TestCoordinatorDefaultGrace(t *testing.T) {
	c := New(0)
	if c.gracePeriod() != 600*time.Second {
		t.Fatalf("default grace = %v", c.gracePeriod())
	}
}

func TestCoordinatorBeginDrainingIdempotent(t *testing.T) {
	c := New(30 * time.Second)
	c.BeginDraining()
	c.BeginDraining()
}

func TestCoordinatorGraceExpired(t *testing.T) {
	c := &Coordinator{grace: time.Second}
	c.BeginDraining()
	c.mu.Lock()
	c.startedAt = time.Now().Add(-2 * time.Second)
	c.mu.Unlock()
	if rem := c.GraceRemainingSeconds(); rem != 0 {
		t.Fatalf("expired grace = %d, want 0", rem)
	}
}
