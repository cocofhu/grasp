package sandbox

import (
	"context"
	"testing"
)

func TestBundleIDFromURLAndBuildInject(t *testing.T) {
	if got := bundleIDFromURL(""); got != "" {
		t.Fatalf("empty: %q", got)
	}
	if got := bundleIDFromURL("https://h/sandbox-inject/abc.tgz"); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := bundleIDFromURL("no-slash"); got != "" {
		t.Fatalf("no slash: %q", got)
	}

	m := NewManager(nil, ManagerOptions{})
	if m.buildInjectConfig("", "/cfg") != nil {
		t.Fatal("empty hostDir")
	}
	if m.buildInjectConfig("/tmp/x", "/cfg") != nil {
		t.Fatal("nil bundles")
	}

	store := NewBundleStore()
	m.bundles = store
	m.injectAdvertiseFallback = "http://127.0.0.1:8080"
	dir := t.TempDir()
	inj := m.buildInjectConfig(dir, "/root/.cursor")
	if inj == nil || inj.BundleURL == "" || inj.Headers == "" {
		t.Fatalf("inject: %+v", inj)
	}
}

func TestManagerAttachDestroyStatusBranches(t *testing.T) {
	gw, fg := newInlineGW(t)
	fg.seed("att-1", "running")
	fg.mu.Lock()
	if rec := fg.recs["att-1"]; rec != nil {
		rec["endpoints"] = map[string]string{"session": "10.0.0.9:1"}
	}
	fg.mu.Unlock()
	m := NewManager(gw, ManagerOptions{WorkspaceDir: "/ws"})
	ctx := context.Background()

	sb, err := m.Attach(ctx, "att-1")
	if err != nil || sb == nil || sb.ID == "" {
		t.Fatalf("Attach: %+v %v", sb, err)
	}
	names, err := m.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range names {
		if n == "att-1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("List missing att-1: %v", names)
	}
	sts, err := m.ListStatuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sts["att-1"] == "" {
		t.Fatalf("ListStatuses missing att-1: %v", sts)
	}

	if err := m.DestroyByName(ctx, "att-1"); err != nil {
		t.Fatal(err)
	}
	_ = m.DestroyByName(ctx, "")
	_ = m.DestroyByName(ctx, "   ")

	mNil := NewManager(nil, ManagerOptions{})
	if _, err := mNil.Attach(ctx, "x"); err == nil {
		t.Fatal("nil gw Attach")
	}
	if st := mNil.Status(ctx, "x"); st != "not_found" {
		t.Fatalf("nil status: %s", st)
	}
	if st := m.Status(ctx, ""); st != "not_found" {
		t.Fatalf("empty id status: %s", st)
	}
}

func TestConfigHomePresentWithoutHelpers(t *testing.T) {
	m := NewManager(nil, ManagerOptions{})
	if !m.configHomePresent(context.Background(), nil, "/cfg") {
		t.Fatal("without installHelpers should short-circuit true")
	}
	var nilM *Manager
	if !nilM.configHomePresent(context.Background(), nil, "/cfg") {
		t.Fatal("nil manager")
	}
}

func TestSandboxFromGWAndCreds(t *testing.T) {
	m := NewManager(nil, ManagerOptions{WorkspaceDir: "/ws", SSHUser: "u"})
	gw := &GWSandbox{
		ID: "id1", Status: "running",
		Endpoints: map[string]string{"session": "1.2.3.4:9", "ssh": "1.2.3.4:22"},
	}
	sb := m.sandboxFromGW(gw, "/custom")
	if sb == nil || sb.ID != "id1" || sb.WorkspaceDir != "/custom" {
		t.Fatal("sandbox handle mismatch")
	}
	c := m.creds("h", 22)
	if c.user != "u" || c.password != "" || c.host != "h" || c.port != 22 {
		t.Fatalf("user=%s host=%s port=%d passwordSet=%t", c.user, c.host, c.port, c.password != "")
	}
	sb.Password = "sandbox-token-value"
	withToken := sb.creds()
	if withToken.password != "sandbox-token-value" || withToken.user != "u" {
		t.Fatal("data-plane password was not the sandbox token")
	}
}
