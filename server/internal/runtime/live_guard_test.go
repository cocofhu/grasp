package runtime

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestParseLiveMarkerSIDs(t *testing.T) {
	out := "data-grasp-live=\"sid002\"\ndata-grasp-live=\"sid001\"\nnoise\ndata-grasp-live=\"sid002\"\ndata-grasp-live=\"bad sid\""
	if got := parseLiveMarkerSIDs(out); !reflect.DeepEqual(got, []string{"sid001", "sid002"}) {
		t.Fatalf("sids = %v", got)
	}
	if got := parseLiveMarkerSIDs(""); got != nil {
		t.Fatalf("empty = %v", got)
	}
}

func TestLiveGuardScript(t *testing.T) {
	s := liveGuardScript("/root/workspace")
	for _, want := range []string{"grasp-live-guard", "'/root/workspace'/*", ".git/hooks/pre-commit", "chmod +x"} {
		if !strings.Contains(s, want) {
			t.Errorf("script missing %q:\n%s", want, s)
		}
	}
	if !strings.Contains(liveGuardHook, "data-grasp-(live|variant)=") {
		t.Error("hook pattern")
	}
}

func TestLiveMarkerSIDsNotParked(t *testing.T) {
	c := &acpProvider{sessions: map[string]*reactSession{}}
	if sids, ok, err := c.LiveMarkerSIDs(context.Background(), "r", "n"); ok || err != nil || sids != nil {
		t.Fatalf("not parked: %v %v %v", sids, ok, err)
	}
	c.InstallLiveGuard(context.Background(), "r", "n") // no-op without a session
}
