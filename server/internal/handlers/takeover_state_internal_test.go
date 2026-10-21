package handlers

import (
	"testing"

	"github.com/cocofhu/grasp/internal/services"
)

// TestDeriveCurrentNodeStateBusy: busy only for a parked manual node whose
// session turn is running; auto running / stopping are never busy.
func TestDeriveCurrentNodeStateBusy(t *testing.T) {
	busy := func() bool { return true }
	idle := func() bool { return false }
	cases := []struct {
		in   services.CurrentNode
		fn   func() bool
		want string
	}{
		{services.CurrentNode{Mode: "manual", State: "waiting_human"}, busy, "busy"},
		{services.CurrentNode{Mode: "manual", State: "waiting_human"}, idle, "waiting_human"},
		{services.CurrentNode{Mode: "manual", State: "stopping"}, busy, "stopping"},
		{services.CurrentNode{Mode: "auto", State: "running"}, busy, "running"},
	}
	for _, tc := range cases {
		if got := deriveCurrentNodeState(tc.in, tc.fn).State; got != tc.want {
			t.Fatalf("%+v: got %s want %s", tc.in, got, tc.want)
		}
	}
}

// TestParseRunListSortLastActivity: the handler whitelist accepts
// sort=last_activity with asc/desc only.
func TestParseRunListSortLastActivity(t *testing.T) {
	for _, o := range []string{"desc", "asc", "DESC"} {
		s, ord := parseRunListSort("last_activity", o)
		if s != "last_activity" || (ord != "desc" && ord != "asc") {
			t.Fatalf("order %s: got %q %q", o, s, ord)
		}
	}
	if s, o := parseRunListSort("last_activity", ""); s != "" || o != "" {
		t.Fatalf("missing order must fall back, got %q %q", s, o)
	}
}
