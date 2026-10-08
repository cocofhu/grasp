package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/browser"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/sandbox/sandboxtest"
)

func TestPreviewServiceCRUD(t *testing.T) {
	db := newTestDB(t)
	svc := NewPreviewService(db, nil)

	rec := mcp.PreviewPort{
		RunID: "r1", NodeID: "n1", Port: 3000, Label: "web",
		ProxyURL: "/preview/r1/n1/3000/", Healthy: true, RegisteredAt: time.Now(),
	}
	if err := svc.UpsertPreviewPort(rec); err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListPreviewPorts("r1", "n1")
	if err != nil || len(list) != 1 || list[0].Port != 3000 {
		t.Fatalf("list: %+v err=%v", list, err)
	}
	got, ok := svc.GetPreviewPort("r1", "n1", 3000)
	if !ok || got.Label != "web" {
		t.Fatalf("get: %+v ok=%v", got, ok)
	}
	if _, ok := svc.GetPreviewPort("r1", "n1", 9999); ok {
		t.Fatal("missing port should be false")
	}

	rec.Label = "web2"
	rec.Healthy = false
	if err := svc.UpsertPreviewPort(rec); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.GetPreviewPort("r1", "n1", 3000)
	if got.Label != "web2" {
		t.Fatalf("upsert update label=%q", got.Label)
	}
	if err := svc.UpdatePreviewHealth("r1", "n1", 3000, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdatePreviewHost("r1", "n1", 3000, "http://10.0.0.1:3000"); err != nil {
		t.Fatal(err)
	}
	got, _ = svc.GetPreviewPort("r1", "n1", 3000)
	if !got.Healthy || got.Host != "http://10.0.0.1:3000" {
		t.Fatalf("after updates: %+v", got)
	}
}

func TestPreviewServiceNilManagerPaths(t *testing.T) {
	db := newTestDB(t)
	svc := NewPreviewService(db, nil)
	svc.SetBrowser(nil)

	svc.ShowPreviewOnDesktop("", 3000)
	svc.ShowPreviewOnDesktop("sb", 3000) // no-op without browser

	if _, err := svc.ContainerIP(context.Background(), "x"); err == nil {
		t.Fatal("nil mgr should error")
	}
	if _, ok := svc.PreviewUpstream(context.Background(), "x", 80); ok {
		t.Fatal("nil mgr upstream should fail")
	}
	if svc.ProbeHTTPPort(context.Background(), "x", 80) {
		t.Fatal("nil mgr probe should be false")
	}
	if pid, err := svc.KeepalivePort(context.Background(), "x", 80); err != nil || pid != 0 {
		t.Fatalf("keepalive nil mgr: pid=%d err=%v", pid, err)
	}
	if name, ok := svc.SandboxForRunNode("r", "n"); ok || name != "" {
		t.Fatalf("no sandbox row: %q ok=%v", name, ok)
	}
}

func TestPreviewServiceWithFakeGateway(t *testing.T) {
	db := newTestDB(t)
	fg := sandboxtest.New(t)
	fg.Seed("sb-prev")
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	svc := NewPreviewService(db, mgr)

	ip, err := svc.ContainerIP(context.Background(), "sb-prev")
	if err != nil {
		t.Fatalf("ContainerIP: %v", err)
	}
	if ip == "" {
		t.Fatal("empty ContainerIP")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	hostPort := srv.Listener.Addr().String()
	fg.SetEndpoints("sb-prev", map[string]string{
		"session": "127.0.0.1:34567",
		"ssh":     "127.0.0.1:2222",
		"3000":    hostPort,
	})
	up, ok := svc.PreviewUpstream(context.Background(), "sb-prev", 3000)
	if !ok || up != "http://"+hostPort {
		t.Fatalf("PreviewUpstream = %q ok=%v", up, ok)
	}
	if !svc.ProbeHTTPPort(context.Background(), "sb-prev", 3000) {
		t.Fatal("expected healthy probe")
	}
	if svc.ProbeHTTPPort(context.Background(), "missing", 3000) {
		t.Fatal("missing sandbox probe should fail")
	}
	if _, err := svc.KeepalivePort(context.Background(), "sb-prev", 3000); err != nil {
		t.Logf("keepalive (may need exec hook): %v", err)
	}

	row := models.Sandbox{
		Name: "sb-prev", RunID: "run-p", NodeID: "node-p",
		Purpose: "run", Status: "running",
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	name, ok := svc.SandboxForRunNode("run-p", "node-p")
	if !ok || name != "sb-prev" {
		t.Fatalf("SandboxForRunNode = %q ok=%v", name, ok)
	}

	bsvc := browser.New(&previewSbxExec{}, browser.Config{})
	bsvc.SetReadyProbe(func(context.Context, string) bool { return true })
	svc.SetBrowser(bsvc)
	svc.ShowPreviewOnDesktop("sb-prev", 0) // no port: no-op
	svc.ShowPreviewOnDesktop("sb-prev", 3000)
	time.Sleep(50 * time.Millisecond)
}

func TestPreviewServiceDirectPreview(t *testing.T) {
	db := newTestDB(t)
	svc := NewPreviewService(db, nil)
	run := models.Run{
		ID: "run-d", Status: "running",
		Graph: models.Graph{Nodes: []models.Node{
			agentNode("n1", "预览", testReviewCaps),
			agentNode("n2", "实现", testAutoCaps),
		}},
	}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if !svc.DirectPreview("run-d", "n1") {
		t.Fatal("set_preview Agent should be direct")
	}
	if svc.DirectPreview("run-d", "n2") {
		t.Fatal("Agent without set_preview is not direct")
	}
	if svc.DirectPreview("missing", "n1") {
		t.Fatal("missing run")
	}
}

type previewSbxExec struct{}

func (previewSbxExec) Exec(context.Context, string, time.Duration, ...string) (string, error) {
	return "", nil
}
