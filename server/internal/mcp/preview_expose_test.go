package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/previewexpose"
)

type connFakePreviewOps struct {
	fakePreviewOps
	conns map[string]struct {
		id    string
		known bool
	}
	direct bool
}

func (f *connFakePreviewOps) DirectPreview(_, _ string) bool { return f.direct }

func (f *connFakePreviewOps) PreviewConnection(sandboxName string) (string, bool) {
	if c, ok := f.conns[sandboxName]; ok {
		return c.id, c.known
	}
	return "", false
}

func TestPreviewExposeSubdomainLifecycleAndReuse(t *testing.T) {
	pol, _, err := previewexpose.NewPolicy(previewexpose.PolicyOptions{
		PublicAdvertise: "https://grasp.example.com",
		Production:      true,
		Connections: []previewexpose.ConnSpec{
			{ConnID: "", Transport: "direct"},
			{ConnID: "e2b", Transport: "gateway", BaseURL: "https://e2b.other-site.com"},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}

	h := NewHost(&memStore{})
	ops := &connFakePreviewOps{
		fakePreviewOps: fakePreviewOps{name: "sb-e2b", ok: true, healthy: true, up: "http://127.0.0.1:43210"},
		direct:         true,
		conns: map[string]struct {
			id    string
			known bool
		}{
			"sb-e2b":     {id: "e2b", known: true},
			"sb-e2b-new": {id: "e2b", known: true},
			"sb-direct":  {id: "", known: true},
		},
	}
	h.SetPreviewSandboxOps(ops)
	h.SetPreviewExposePolicy(pol)
	tok := grantScope(h, "r1")
	h.SetActiveNode("r1", "n1", capsPreview)

	// 1. First set_preview mints a 32-hex subdomain key.
	resp := callPreviewTool(t, h, "r1", tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"set_preview","arguments":{"port":5173,"label":"App"}}}`)
	if previewResultIsError(resp) {
		t.Fatalf("set_preview failed: %s", previewResultText(t, resp))
	}
	p1 := activePreviewOrFail(t, h, "r1", "n1")
	if p1.ExposeStrategy != previewexpose.StrategySubdomain || !previewexpose.ValidKey(p1.ExposeKey) {
		t.Fatalf("unexpected expose binding: %+v", p1)
	}
	wantURL := "https://" + p1.ExposeKey + ".e2b.other-site.com/"
	if p1.DirectURL != wantURL {
		t.Fatalf("DirectURL = %q, want %q", p1.DirectURL, wantURL)
	}
	if got, ok := h.PreviewByExposeKey(p1.ExposeKey); !ok || got.RunID != "r1" || got.DirectURL != wantURL {
		t.Fatalf("PreviewByExposeKey failed: ok=%v got=%+v", ok, got)
	}

	// 2. Re-registering same sandbox + same port reuses the existing key (Live reload stability).
	callPreviewTool(t, h, "r1", tok, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"set_preview","arguments":{"port":5173,"label":"App v2"}}}`)
	p2 := activePreviewOrFail(t, h, "r1", "n1")
	if p2.ExposeKey != p1.ExposeKey {
		t.Fatalf("expected key reuse on same sandbox+port, got %q != %q", p2.ExposeKey, p1.ExposeKey)
	}

	// 3. Changing port mints a fresh key and invalidates the old key.
	callPreviewTool(t, h, "r1", tok, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"set_preview","arguments":{"port":8080,"label":"API"}}}`)
	p3 := activePreviewOrFail(t, h, "r1", "n1")
	if p3.ExposeKey == p1.ExposeKey || !previewexpose.ValidKey(p3.ExposeKey) {
		t.Fatalf("expected rotated key on port change, got %q", p3.ExposeKey)
	}
	if _, ok := h.PreviewByExposeKey(p1.ExposeKey); ok {
		t.Fatal("old key must not remain resolvable after port change")
	}

	// 4. Changing sandbox mints a fresh key.
	ops.name = "sb-e2b-new"
	callPreviewTool(t, h, "r1", tok, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"set_preview","arguments":{"port":8080,"label":"API"}}}`)
	p4 := activePreviewOrFail(t, h, "r1", "n1")
	if p4.ExposeKey == p3.ExposeKey {
		t.Fatal("expected rotated key on sandbox change")
	}

	// 5. Updating base_url recomputes DirectURL with the existing key without migration.
	polNewBase, _, err := previewexpose.NewPolicy(previewexpose.PolicyOptions{
		PublicAdvertise: "https://grasp.example.com",
		Production:      true,
		Connections: []previewexpose.ConnSpec{
			{ConnID: "", Transport: "direct"},
			{ConnID: "e2b", Transport: "gateway", BaseURL: "https://moved.third-site.com"},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy moved: %v", err)
	}
	h.SetPreviewExposePolicy(polNewBase)
	pMoved := activePreviewOrFail(t, h, "r1", "n1")
	if pMoved.DirectURL != "https://"+p4.ExposeKey+".moved.third-site.com/" {
		t.Fatalf("DirectURL after base_url change = %q", pMoved.DirectURL)
	}

	// 6. Switching strategy to none fails closed.
	polNone, _, _ := previewexpose.NewPolicy(previewexpose.PolicyOptions{
		PublicAdvertise: "https://grasp.example.com",
		Connections:     []previewexpose.ConnSpec{{ConnID: "e2b", Transport: "gateway", Strategy: previewexpose.StrategyNone}},
	})
	h.SetPreviewExposePolicy(polNone)
	if pClosed := activePreviewOrFail(t, h, "r1", "n1"); pClosed.DirectURL != "" {
		t.Fatalf("expected empty DirectURL after strategy switch to none, got %q", pClosed.DirectURL)
	}
}

func TestPreviewExposeLegacyRowsMultiGatewayAndUnknownConnection(t *testing.T) {
	pol, _, err := previewexpose.NewPolicy(previewexpose.PolicyOptions{
		PublicAdvertise: "https://grasp.example.com",
		Production:      true,
		Connections: []previewexpose.ConnSpec{
			{ConnID: "", Transport: "direct"},
			{ConnID: "e2b", Transport: "gateway", BaseURL: "https://e2b.other-site.com"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHost(&memStore{})
	ops := &connFakePreviewOps{
		fakePreviewOps: fakePreviewOps{name: "sb-direct", ok: true, healthy: true, up: "http://10.0.0.5:18080"},
		direct:         true,
		conns: map[string]struct {
			id    string
			known bool
		}{
			"sb-direct":  {id: "", known: true},
			"sb-e2b":     {id: "e2b", known: true},
			"sb-unknown": {id: "removed", known: false},
		},
	}
	h.SetPreviewSandboxOps(ops)
	h.SetPreviewExposePolicy(pol)

	// Legacy direct row without ExposeStrategy works without migration.
	h.replacePreviewMem(PreviewPort{
		RunID: "r-dir", NodeID: "n1", Kind: PreviewKindPort, Port: 5173,
		SandboxName: "sb-direct", Host: "http://10.0.0.5:18080", Healthy: true,
	})
	if got := activePreviewOrFail(t, h, "r-dir", "n1"); got.DirectURL != "http://10.0.0.5:18080/" {
		t.Fatalf("legacy direct row DirectURL = %q", got.DirectURL)
	}

	// Rows bound under another strategy stay closed after the connection
	// switches to direct, until set_preview registers them again.
	ticket, err := previewexpose.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	for i, stored := range []previewexpose.Binding{{Strategy: previewexpose.StrategyNone}, {Strategy: previewexpose.StrategySubdomain, Key: ticket}} {
		runID := "r-switched-" + stored.Strategy
		h.replacePreviewMem(PreviewPort{
			RunID: runID, NodeID: "n1", Kind: PreviewKindPort, Port: 5174 + i,
			SandboxName: "sb-direct", Host: "http://10.0.0.5:18080", Healthy: true,
			ExposeStrategy: stored.Strategy, ExposeKey: stored.Key,
		})
		if got := activePreviewOrFail(t, h, runID, "n1"); got.DirectURL != "" {
			t.Fatalf("%s row exposed after switching to direct: %q", stored.Strategy, got.DirectURL)
		}
	}

	// Legacy row on subdomain gateway without ExposeKey does NOT lazy-bind.
	h.replacePreviewMem(PreviewPort{
		RunID: "r-e2b-old", NodeID: "n1", Kind: PreviewKindPort, Port: 5173,
		SandboxName: "sb-e2b", Host: "http://127.0.0.1:43210", Healthy: true,
	})
	if got := activePreviewOrFail(t, h, "r-e2b-old", "n1"); got.DirectURL != "" {
		t.Fatalf("legacy row without key must not lazy-bind, got %q", got.DirectURL)
	}

	// Unknown connection fails closed to none.
	h.replacePreviewMem(PreviewPort{
		RunID: "r-unk", NodeID: "n1", Kind: PreviewKindPort, Port: 5173,
		SandboxName: "sb-unknown", Host: "http://10.0.0.9:18080", Healthy: true,
	})
	if got := activePreviewOrFail(t, h, "r-unk", "n1"); got.DirectURL != "" {
		t.Fatalf("unknown connection must fail closed, got %q", got.DirectURL)
	}
}

func TestPreviewExposeBindDegradesToNoneWithNote(t *testing.T) {
	// Verify that a failing Bind degrades to none with a Note instead of failing set_preview.
	h := NewHost(&memStore{})
	ops := &connFakePreviewOps{
		fakePreviewOps: fakePreviewOps{name: "sb", ok: true, healthy: true, up: "http://127.0.0.1:43210"},
		direct:         true,
		conns: map[string]struct {
			id    string
			known bool
		}{"sb": {id: "", known: true}},
	}
	h.SetPreviewSandboxOps(ops)

	// Use canceled context to force SubdomainExposer.Bind to fail cleanly.
	pol, _, err := previewexpose.NewPolicy(previewexpose.PolicyOptions{
		PublicAdvertise: "http://grasp.example.com",
		Connections:     []previewexpose.ConnSpec{{ConnID: "", Transport: "gateway", BaseURL: "http://sub.other.test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.SetPreviewExposePolicy(pol)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	reg, err := h.savePreviewPort(canceled, ops, "r1", "n1", "sb", 5173, 0, "App", "http://127.0.0.1:43210", 123)
	if err != nil {
		t.Fatalf("savePreviewPort must degrade instead of failing: %v", err)
	}
	if reg.PublicURL != "" || !strings.Contains(reg.Note, "降级") {
		t.Fatalf("expected degraded registration with note, got %+v", reg)
	}
	p := activePreviewOrFail(t, h, "r1", "n1")
	if p.ExposeStrategy != previewexpose.StrategyNone || p.DirectURL != "" {
		t.Fatalf("expected stored strategy none, got %+v", p)
	}
}
