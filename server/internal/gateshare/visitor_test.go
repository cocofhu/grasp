package gateshare

import (
	"strings"
	"testing"
)

func TestVisitorLane(t *testing.T) {
	v := strings.Repeat("ab", 16)
	lane := VisitorLane("link1", v)
	if !strings.HasPrefix(lane, "v:") || len(lane) != 14 {
		t.Fatalf("lane = %q", lane)
	}
	if VisitorLane("link1", v) != lane {
		t.Fatal("lane must be stable for one browser")
	}
	if VisitorLane("link2", v) == lane {
		t.Fatal("the same browser must get unrelated lanes on different links")
	}
	if VisitorLane("link1", strings.Repeat("cd", 16)) == lane {
		t.Fatal("different browsers must get different lanes")
	}
	if strings.Contains(lane, v[:12]) {
		t.Fatal("lane must not carry the raw visitor id")
	}
	for _, bad := range []string{"", "xyz", strings.Repeat("a", 31), strings.Repeat("g", 32)} {
		if VisitorLane("link1", bad) != "" {
			t.Fatalf("invalid visitor %q produced a lane", bad)
		}
	}
	if VisitorLane("", v) != "" {
		t.Fatal("empty link id produced a lane")
	}
}

func TestFilterPublicLaneFrame(t *testing.T) {
	own := []byte(`{"type":"visitor","kind":"review","lane":"v:aaa","runId":"r1","nodeId":"p1","event":"turn_done"}`)
	ownAcp := []byte(`{"type":"visitor","kind":"acp","lane":"v:aaa","runId":"r1","nodeId":"p1","events":[{"kind":"message","text":"hi"}],"busy":true}`)
	other := []byte(`{"type":"visitor","kind":"review","lane":"v:bbb","runId":"r1","nodeId":"p1","event":"turn_done"}`)
	node := []byte(`{"type":"review","runId":"r1","nodeId":"p1","event":"turn_done"}`)
	nodeAcp := []byte(`{"type":"acp","runId":"r1","nodeId":"p1","events":[{"kind":"message","text":"owner"}]}`)
	live := []byte(`{"type":"live","runId":"r1","nodeId":"p1","session":{"sid":"s1","state":"ready"}}`)

	out, ok := FilterPublicLaneFrame(own, "p1", "v:aaa", base(0))
	if !ok {
		t.Fatal("own review frame dropped")
	}
	if s := string(out); !strings.Contains(s, `"type":"review"`) || strings.Contains(s, "v:aaa") || strings.Contains(s, "r1") {
		t.Fatalf("own frame not unwrapped / sanitized: %s", s)
	}
	out, ok = FilterPublicLaneFrame(ownAcp, "p1", "v:aaa", base(0))
	if !ok || !strings.Contains(string(out), `"type":"acp"`) {
		t.Fatalf("own acp frame: %s %v", out, ok)
	}
	for name, raw := range map[string][]byte{"other visitor": other, "node review": node, "node acp": nodeAcp} {
		if _, ok := FilterPublicLaneFrame(raw, "p1", "v:aaa", base(0)); ok {
			t.Fatalf("%s frame leaked to a visitor", name)
		}
	}
	if _, ok := FilterPublicLaneFrame(live, "p1", "v:aaa", base(0)); !ok {
		t.Fatal("node-wide live frame should reach visitors")
	}
	for name, raw := range map[string][]byte{"visitor": own, "visitor acp": ownAcp} {
		if _, ok := FilterPublicBrokerFrame(raw, "p1", base(0)); ok {
			t.Fatalf("%s frame leaked to the default lane", name)
		}
	}
	if _, ok := FilterPublicBrokerFrame(node, "p1", base(0)); !ok {
		t.Fatal("default lane lost its own frame")
	}
}
