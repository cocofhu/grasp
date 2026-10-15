package mcp

import (
	"fmt"
	"testing"
)

func TestClosedSessionTombstonesEvictOldest(t *testing.T) {
	h := &Host{}
	for i := 0; i < maxClosedToolSessions; i++ {
		h.deleteToolSession("run-1", fmt.Sprintf("sid-%d", i))
	}
	h.deleteToolSession("run-1", "sid-new")
	closed := h.closedSessions["run-1"]
	if len(closed) != maxClosedToolSessions {
		t.Fatalf("tombstones = %d, want cap %d", len(closed), maxClosedToolSessions)
	}
	if _, ok := closed["sid-new"]; !ok {
		t.Fatal("newest DELETEd session must be tombstoned even when full")
	}
	if _, ok := closed["sid-0"]; ok {
		t.Fatal("oldest tombstone should have been evicted")
	}
	// Re-deleting a tombstoned sid does not evict anything.
	h.deleteToolSession("run-1", "sid-new")
	if _, ok := closed["sid-1"]; !ok || len(closed) != maxClosedToolSessions {
		t.Fatal("re-DELETE of a tombstoned sid evicted another tombstone")
	}
}
