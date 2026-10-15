package provider

import (
	"sort"
	"testing"
)

func TestApplyEnvOverlayOverridesAndSkipsReserved(t *testing.T) {
	base := []string{"PATH=/bin", "CURSOR_API_KEY=old", "AGENT_PROVIDER=cursor", "HOME=/root"}
	out := ApplyEnvOverlay(base, map[string]string{
		"CURSOR_API_KEY":      "new",
		"AGENT_PROVIDER":      "evil",
		"ACP_BRIDGE_PASSWORD": "leak",
		"NODE_ONLY":           "1",
		"GRASP_NODE_ID":       "n1",
	})
	got := map[string]string{}
	for _, kv := range out {
		i := indexEq(kv)
		if i < 0 {
			t.Fatalf("bad entry %q", kv)
		}
		got[kv[:i]] = kv[i+1:]
	}
	if got["CURSOR_API_KEY"] != "new" {
		t.Fatalf("CURSOR_API_KEY=%q", got["CURSOR_API_KEY"])
	}
	if got["AGENT_PROVIDER"] != "cursor" {
		t.Fatalf("reserved AGENT_PROVIDER overridden: %q", got["AGENT_PROVIDER"])
	}
	if _, ok := got["ACP_BRIDGE_PASSWORD"]; ok {
		t.Fatal("reserved ACP_BRIDGE_PASSWORD must not be injected by overlay")
	}
	if got["NODE_ONLY"] != "1" || got["GRASP_NODE_ID"] != "n1" {
		t.Fatalf("node keys missing: %#v", got)
	}
}

func TestSanitizeSessionEnvDropsReserved(t *testing.T) {
	got := SanitizeSessionEnv(map[string]string{
		"CURSOR_API_KEY": "k",
		"CONFIG_ROOT":    "/evil",
		"GRASP_NODE_ID":  "n2",
	})
	if got["CURSOR_API_KEY"] != "k" || got["GRASP_NODE_ID"] != "n2" {
		t.Fatalf("got %#v", got)
	}
	if _, ok := got["CONFIG_ROOT"]; ok {
		t.Fatal("CONFIG_ROOT must be stripped")
	}
}

func TestSessionEnvEqual(t *testing.T) {
	a := map[string]string{"A": "1", "CONFIG_ROOT": "x"}
	b := map[string]string{"A": "1"}
	if !SessionEnvEqual(a, b) {
		t.Fatal("reserved-only diff should still be equal after sanitize")
	}
	if SessionEnvEqual(a, map[string]string{"A": "2"}) {
		t.Fatal("value mismatch")
	}
}

func indexEq(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return i
		}
	}
	return -1
}

func TestApplyEnvOverlayStableKeys(t *testing.T) {
	out := ApplyEnvOverlay([]string{"Z=1"}, map[string]string{"A": "2", "B": "3"})
	keys := make([]string, 0, len(out))
	for _, kv := range out {
		keys = append(keys, kv[:indexEq(kv)])
	}
	sort.Strings(keys)
	if len(keys) != 3 {
		t.Fatalf("keys=%v", keys)
	}
}
