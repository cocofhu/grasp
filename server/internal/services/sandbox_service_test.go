package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/sandbox/sandboxtest"

	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

// dockerState wraps a fake sandbox-gateway so the service tests can drive
// sandbox statuses without a real gateway or Docker. It keeps the field surface
// (acpPort/failRun/failPs) the tests were written against; setStatus translates
// docker-vocab statuses to the gateway's. A DB row's Name is the gateway id, so
// setStatus(name, …) registers/looks up sandboxes by that id.
type dockerState struct {
	fg      *sandboxtest.FakeGateway
	acpPort int  // session-endpoint port for created/seeded sandboxes (0 -> 34567)
	failRun bool // when true, gateway Create returns an error
	failPs  bool // when true, gateway List returns an error
}

// setStatus registers/updates a sandbox by id with a gateway status. Docker
// vocab is translated ("exited" -> "stopped", "creating" -> "pending").
func (d *dockerState) setStatus(name, status string) {
	switch status {
	case "exited":
		status = "stopped"
	case "creating":
		status = "pending"
	}
	d.fg.SetStatus(name, status)
}

func newSandboxService(t *testing.T, db *gorm.DB, ds *dockerState) *SandboxService {
	t.Helper()
	fg := sandboxtest.New(t)
	fg.ACPPort = ds.acpPort
	fg.FailCreate = ds.failRun
	fg.FailList = ds.failPs
	ds.fg = fg
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	skillsRoot := t.TempDir()
	skills := NewAgentService(skillsRoot)
	// Create an agent so Open can resolve a profile.
	if err := skills.Save(Agent{Name: "agentA", AcpBackend: AcpBackendCursor, Files: []AgentFile{{Path: "rules/a.md", Content: "# a"}}}); err != nil {
		t.Fatal(err)
	}
	host := mcp.NewHost(NewArtifactService(db))
	return NewSandboxService(db, mgr, skills, host, SandboxOptions{
		ProfilesRoot: skillsRoot,
		MCPEndpoint:  "http://mcp.local",
		TTL:          time.Minute,
		RunTTL:       time.Minute,
		Max:          2,
		ProjectCredentials: func(string) map[string]string {
			return map[string]string{"GRASP_CURSOR_API_KEY": "test-key", "GRASP_OPENCODE_API_KEY": "sk-test"}
		},
	})
}

func TestSandboxServiceDefaults(t *testing.T) {
	db := newTestDB(t)
	fg := sandboxtest.New(t)
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{})
	skills := NewAgentService(t.TempDir())
	host := mcp.NewHost(NewArtifactService(db))
	// Zero options -> defaults filled in.
	s := NewSandboxService(db, mgr, skills, host, SandboxOptions{})
	if s.TTL() <= 0 || s.MaxTestSandboxes() <= 0 || s.chatTimeout <= 0 || s.RunTTL() <= 0 {
		t.Fatalf("defaults not applied: %+v", s)
	}
}

func TestRunSandboxLifecycle(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	// Empty name is a no-op for all three.
	s.RegisterRunSandbox(runtimeInfo(""))
	s.RetireRunSandbox("")
	s.UnregisterRunSandbox("")

	name := "grasp-sb-run1"
	ds.setStatus(name, "running")
	s.RegisterRunSandbox(runtimeInfo(name))

	// Row created, active (busy) in view.
	var row models.Sandbox
	if err := db.Where("name = ?", name).First(&row).Error; err != nil {
		t.Fatalf("register row: %v", err)
	}
	if row.Purpose != "run" || row.Status != "running" {
		t.Fatalf("row: %+v", row)
	}
	v := s.view(ctx, &row)
	if !v.Busy {
		t.Fatal("run sandbox should be busy while active")
	}
	if !s.isBusyRow(&row) {
		t.Fatal("isBusyRow should be true")
	}

	// Retire clears active flag and sets a destroy deadline.
	s.RetireRunSandbox(name)
	db.Where("name = ?", name).First(&row)
	if row.DestroyAt == nil {
		t.Fatal("retire should set destroy_at")
	}
	if s.isBusyRow(&row) {
		t.Fatal("retired sandbox no longer active")
	}

	// Unregister deletes the row.
	s.UnregisterRunSandbox(name)
	if err := db.Where("name = ?", name).First(&models.Sandbox{}).Error; err != gorm.ErrRecordNotFound {
		t.Fatalf("row should be deleted, got %v", err)
	}
}

func runtimeInfo(name string) runtime.RunSandboxInfo {
	return runtime.RunSandboxInfo{
		Name: name, Profile: "agentA", RunID: "run-" + name, NodeID: "impl",
		Host: "127.0.0.1", ACPPort: 8765,
	}
}

func TestSandboxListGetView(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	if _, err := s.Get(999); err == nil {
		t.Fatal("get missing should error")
	}
	if _, err := s.GetView(ctx, 999); err == nil {
		t.Fatal("getview missing should error")
	}

	row := &models.Sandbox{Name: "grasp-sb-t1", Purpose: "test", Status: "running", ACPPort: 1, CodeServerPort: 2}
	db.Create(row)
	ds.setStatus(row.Name, "running")
	ds.fg.SetEndpoints(row.Name, map[string]string{
		"session": "10.0.0.1:30101",
		"ide":     "10.0.0.1:30102",
		"ssh":     "10.0.0.1:22",
		"cdp":     "10.0.0.1:9222",
		"novnc":   "10.0.0.1:6080",
		"9222":    "10.0.0.1:9222",
		"6080":    "10.0.0.1:6080",
		"8080":    "10.0.0.1:30880",
	})

	got, err := s.Get(row.ID)
	if err != nil || got.Name != row.Name {
		t.Fatalf("get: %+v %v", got, err)
	}
	gv, err := s.GetView(ctx, row.ID)
	if err != nil || gv.ContainerStatus != "running" || !gv.HasACP || !gv.HasCodeServer {
		t.Fatalf("getview: %+v %v", gv, err)
	}
	if gv.Endpoints["session"] != "10.0.0.1:30101" || gv.Endpoints["ide"] != "10.0.0.1:30102" || gv.Endpoints["ssh"] != "10.0.0.1:22" {
		t.Fatalf("getview endpoints = %#v", gv.Endpoints)
	}
	if len(gv.Endpoints) != 3 {
		t.Fatalf("getview must only expose session/ide/ssh, got %#v", gv.Endpoints)
	}
	for _, banned := range []string{"cdp", "novnc", "9222", "6080", "8080"} {
		if _, ok := gv.Endpoints[banned]; ok {
			t.Fatalf("getview leaked sensitive endpoint %q: %#v", banned, gv.Endpoints)
		}
	}
	list, err := s.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if list[0].ContainerStatus != "running" {
		t.Fatalf("list containerStatus = %q, want running", list[0].ContainerStatus)
	}
	if list[0].Endpoints != nil {
		t.Fatalf("list must not attach endpoints, got %#v", list[0].Endpoints)
	}
}

func TestPublicSandboxEndpointsWhitelist(t *testing.T) {
	in := map[string]string{
		"session": "10.0.0.1:8765",
		"ide":     "10.0.0.1:8744",
		"ssh":     "10.0.0.1:22",
		"cdp":     "10.0.0.1:9222",
		"novnc":   "10.0.0.1:6080",
		"9222":    "10.0.0.1:9222",
		"6080":    "10.0.0.1:6080",
		"8080":    "10.0.0.1:8080",
	}
	got := publicSandboxEndpoints(in)
	if len(got) != 3 || got["session"] != "10.0.0.1:8765" || got["ide"] != "10.0.0.1:8744" || got["ssh"] != "10.0.0.1:22" {
		t.Fatalf("whitelist = %#v", got)
	}
	// Named key still dropped if the value is a sensitive host:port.
	got = publicSandboxEndpoints(map[string]string{"session": "203.0.113.10:9222", "ide": "203.0.113.10:8744"})
	if _, ok := got["session"]; ok || got["ide"] != "203.0.113.10:8744" || len(got) != 1 {
		t.Fatalf("sensitive value not filtered: %#v", got)
	}
	if got := publicSandboxEndpoints(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil in should yield empty map, got %#v", got)
	}
}

func TestSandboxGetViewEndpointsDegrade(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	row := &models.Sandbox{Name: "grasp-sb-ep-fail", Purpose: "test", Status: "running"}
	db.Create(row)
	ds.setStatus(row.Name, "running")
	ds.fg.FailGet = true

	gv, err := s.GetView(ctx, row.ID)
	if err != nil {
		t.Fatalf("GetView should succeed when gateway Get fails: %v", err)
	}
	if gv.Endpoints == nil {
		t.Fatal("endpoints should be empty map, not nil")
	}
	if len(gv.Endpoints) != 0 {
		t.Fatalf("endpoints should be empty on gateway failure, got %#v", gv.Endpoints)
	}
	if gv.ContainerStatus != "running" {
		t.Fatalf("containerStatus = %q, want running", gv.ContainerStatus)
	}
}

func TestSandboxListBatchStatuses(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	running := &models.Sandbox{Name: "grasp-sb-run", Purpose: "test", Status: "running"}
	exited := &models.Sandbox{Name: "grasp-sb-ex", Purpose: "test", Status: "stopped"}
	missing := &models.Sandbox{Name: "grasp-sb-gone", Purpose: "test", Status: "running"}
	db.Create(running)
	db.Create(exited)
	db.Create(missing)
	ds.setStatus(running.Name, "running")
	ds.setStatus(exited.Name, "exited")
	// missing: no setStatus → not in map → not_found

	before := ds.fg.ListCalls
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if ds.fg.ListCalls-before != 1 {
		t.Fatalf("List should trigger exactly one gateway list, got %d", ds.fg.ListCalls-before)
	}
	if len(list) != 3 {
		t.Fatalf("list len=%d", len(list))
	}
	byName := map[string]string{}
	for _, v := range list {
		byName[v.Name] = v.ContainerStatus
	}
	if byName[running.Name] != "running" {
		t.Fatalf("running status = %q", byName[running.Name])
	}
	if byName[exited.Name] != "exited" {
		t.Fatalf("exited status = %q", byName[exited.Name])
	}
	if byName[missing.Name] != "not_found" {
		t.Fatalf("missing status = %q, want not_found", byName[missing.Name])
	}

	// GetView uses a single per-id status call (does not bump the list count).
	psBefore := ds.fg.ListCalls
	gv, err := s.GetView(ctx, running.ID)
	if err != nil || gv.ContainerStatus != "running" {
		t.Fatalf("getview: %+v %v", gv, err)
	}
	if ds.fg.ListCalls != psBefore {
		t.Fatal("GetView should not call the gateway list endpoint")
	}
}

func TestSandboxListDockerFailureDegrades(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{failPs: true}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	db.Create(&models.Sandbox{Name: "grasp-sb-a", Purpose: "test", Status: "running"})
	db.Create(&models.Sandbox{Name: "grasp-sb-b", Purpose: "test", Status: "running"})

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("list should succeed on docker failure: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len=%d", len(list))
	}
	for _, v := range list {
		if v.ContainerStatus != "unknown" {
			t.Fatalf("%s status = %q, want unknown", v.Name, v.ContainerStatus)
		}
	}
}

func TestSandboxListFiftyBatchOnce(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	const n = 50
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("grasp-sb-perf-%02d", i)
		db.Create(&models.Sandbox{Name: name, Purpose: "test", Status: "running"})
		ds.setStatus(name, "running")
	}

	before := ds.fg.ListCalls
	start := time.Now()
	list, err := s.List(ctx)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != n {
		t.Fatalf("list len=%d want %d", len(list), n)
	}
	if ds.fg.ListCalls-before != 1 {
		t.Fatalf("expected 1 gateway list, got %d", ds.fg.ListCalls-before)
	}
	// Stub is near-instant; keep a generous local bound well under the 2s P95 SLO.
	if elapsed > 2*time.Second {
		t.Fatalf("list took %v, want ≤ 2s", elapsed)
	}
	t.Logf("List(%d) took %v with %d gateway list call(s)", n, elapsed, ds.fg.ListCalls-before)
}

func TestSandboxStopDestroyCleanup(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	r1 := &models.Sandbox{Name: "grasp-sb-a", Purpose: "test", Status: "running"}
	db.Create(r1)
	ds.setStatus(r1.Name, "running")
	if err := s.Stop(ctx, r1.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	var got models.Sandbox
	db.First(&got, r1.ID)
	if got.Status != "stopped" {
		t.Fatalf("stopped status = %s", got.Status)
	}

	r2 := &models.Sandbox{Name: "grasp-sb-b", Purpose: "test", Status: "running", RunID: "rr"}
	db.Create(r2)
	if err := s.Destroy(ctx, r2.ID); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if err := db.First(&models.Sandbox{}, r2.ID).Error; err != gorm.ErrRecordNotFound {
		t.Fatal("destroyed row should be gone")
	}
	if e := s.Stop(ctx, 999); e == nil {
		t.Fatal("stop missing should error")
	}
	if e := s.Destroy(ctx, 999); e == nil {
		t.Fatal("destroy missing should error")
	}

	// CleanupIdle destroys remaining non-busy sandboxes.
	db.Create(&models.Sandbox{Name: "grasp-sb-c", Purpose: "test", Status: "running"})
	destroyed, _ := s.CleanupIdle(ctx)
	if destroyed < 1 {
		t.Fatalf("cleanup destroyed=%d", destroyed)
	}
}

func TestSandboxBusyGuards(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	name := "grasp-sb-busy"
	s.RegisterRunSandbox(runtimeInfo(name)) // marks runActive
	var row models.Sandbox
	db.Where("name = ?", name).First(&row)
	if e := s.Stop(ctx, row.ID); e == nil {
		t.Fatal("stop busy should error")
	}
	if e := s.Destroy(ctx, row.ID); e == nil {
		t.Fatal("destroy busy should error")
	}
}

func TestSandboxSweeper(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	db.Create(&models.Sandbox{Name: "grasp-sb-old", Purpose: "test", Status: "running", DestroyAt: &past})
	db.Create(&models.Sandbox{Name: "grasp-sb-keep", Purpose: "test", Status: "running"})
	s.sweepOnce(ctx)
	var n int64
	db.Model(&models.Sandbox{}).Count(&n)
	if n != 1 {
		t.Fatalf("sweeper left %d rows, want 1", n)
	}

	// RunSweeper respects context cancellation.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	s.RunSweeper(cctx) // returns immediately
}

func TestSandboxReconcileOnStartup(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	// run-purpose row -> destroyed on startup.
	db.Create(&models.Sandbox{Name: "grasp-sb-run", Purpose: "run", Status: "running", RunID: "rid"})
	ds.setStatus("grasp-sb-run", "running")
	// test running -> attached and kept.
	db.Create(&models.Sandbox{Name: "grasp-sb-live", Purpose: "test", Status: "running"})
	ds.setStatus("grasp-sb-live", "running")
	// test gone -> dropped.
	db.Create(&models.Sandbox{Name: "grasp-sb-dead", Purpose: "test", Status: "running", RunID: "rid2"})
	// orphan sandbox present on the gateway but with no DB row.
	ds.setStatus("grasp-sb-orphan", "running")

	s.ReconcileOnStartup(ctx)

	var live models.Sandbox
	if err := db.Where("name = ?", "grasp-sb-live").First(&live).Error; err != nil {
		t.Fatal("live row should survive")
	}
	if live.ACPPort == 0 {
		t.Fatal("live row should get refreshed port")
	}
	if err := db.Where("name = ?", "grasp-sb-run").First(&models.Sandbox{}).Error; err != gorm.ErrRecordNotFound {
		t.Fatal("run row should be gone")
	}
	if err := db.Where("name = ?", "grasp-sb-dead").First(&models.Sandbox{}).Error; err != gorm.ErrRecordNotFound {
		t.Fatal("dead row should be gone")
	}
	// g1.1/g1.2: recycled names are Destroy'd; g1.3: unmanaged orphans Destroy'd.
	if ds.fg.Has("grasp-sb-run") {
		t.Fatal("run gateway sandbox should be destroyed (g1.1)")
	}
	if ds.fg.Has("grasp-sb-orphan") {
		t.Fatal("orphan gateway sandbox should be destroyed (g1.3)")
	}
	if !ds.fg.Has("grasp-sb-live") {
		t.Fatal("live gateway sandbox must remain")
	}
}

// TestSandboxReconcileDestroysNonRunningGateway covers g1.1+g1.2: a non-running
// local row whose Name is the gateway id must Destroy the gateway instance
// before the list row is deleted, and must not leave the id in tracked so the
// orphan pass cannot skip it.
func TestSandboxReconcileDestroysNonRunningGateway(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	// error / stopped rows still have a control-plane record protecting resources.
	db.Create(&models.Sandbox{Name: "gw-error", Purpose: "test", Status: "error"})
	ds.setStatus("gw-error", "error")
	db.Create(&models.Sandbox{Name: "gw-stopped", Purpose: "agent", Status: "stopped"})
	ds.setStatus("gw-stopped", "stopped")
	db.Create(&models.Sandbox{Name: "gw-pm-dead", Purpose: "agent", Status: "error"})
	ds.setStatus("gw-pm-dead", "error")

	s.ReconcileOnStartup(ctx)

	for _, name := range []string{"gw-error", "gw-stopped", "gw-pm-dead"} {
		if err := db.Where("name = ?", name).First(&models.Sandbox{}).Error; err != gorm.ErrRecordNotFound {
			t.Fatalf("%s local row should be gone", name)
		}
		if ds.fg.Has(name) {
			t.Fatalf("%s gateway sandbox should be destroyed (g1.1/g1.2)", name)
		}
	}
}

// TestSandboxReconcileCreatingGrace covers g1.3 brief-window protection: a
// young creating row with placeholder Name keeps its correlated gateway
// sandbox (grasp.name label) from orphan Destroy.
func TestSandboxReconcileCreatingGrace(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	placeholder := "grasp-sb-ph-young"
	gwID := "gw-inflight-001"
	db.Create(&models.Sandbox{
		Name: placeholder, Purpose: "test", Status: "creating",
		CreatedAt: time.Now(),
	})
	ds.setStatus(gwID, "pending")
	ds.fg.SetLabel(gwID, sandbox.CorrelationNameKey(), placeholder)

	s.ReconcileOnStartup(ctx)

	if err := db.Where("name = ?", placeholder).First(&models.Sandbox{}).Error; err != nil {
		t.Fatal("young creating row should be kept")
	}
	if !ds.fg.Has(gwID) {
		t.Fatal("correlated in-flight gateway sandbox must not be destroyed")
	}
}

// TestSandboxReconcileAbandonedCreating destroys stale creating rows and the
// gateway sandbox still labeled with the placeholder Name.
func TestSandboxReconcileAbandonedCreating(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	placeholder := "grasp-sb-ph-old"
	gwID := "gw-abandoned-001"
	db.Create(&models.Sandbox{
		Name: placeholder, Purpose: "test", Status: "creating",
		CreatedAt: time.Now().Add(-time.Hour),
	})
	ds.setStatus(gwID, "running")
	ds.fg.SetLabel(gwID, sandbox.CorrelationNameKey(), placeholder)

	s.ReconcileOnStartup(ctx)

	if err := db.Where("name = ?", placeholder).First(&models.Sandbox{}).Error; err != gorm.ErrRecordNotFound {
		t.Fatal("abandoned creating row should be gone")
	}
	if ds.fg.Has(gwID) {
		t.Fatal("correlated abandoned gateway sandbox should be destroyed")
	}
}

func TestSandboxOpenPaths(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	// Unknown agent.
	if _, err := s.Open(ctx, "ghost", nil, ""); err == nil {
		t.Fatal("open unknown agent should error")
	}

	// Reuse: seed a running test sandbox with empty repo for agentA.
	reuse := &models.Sandbox{Name: "grasp-sb-reuse", Purpose: "test", Profile: "agentA", Status: "running", RepoURL: ""}
	db.Create(reuse)
	ds.setStatus(reuse.Name, "running")
	row, err := s.Open(ctx, "agentA", nil, "")
	if err != nil {
		t.Fatalf("open reuse: %v", err)
	}
	if row.ID != reuse.ID {
		t.Fatalf("should reuse existing, got id %d want %d", row.ID, reuse.ID)
	}

	// Cap reached: fill with repo-backed running sandboxes (not reusable) up to max.
	db.Model(&models.Sandbox{}).Where("id = ?", reuse.ID).Update("repo_url", "http://repo")
	db.Create(&models.Sandbox{Name: "grasp-sb-x2", Purpose: "test", Profile: "agentA", Status: "running", RepoURL: "http://repo2"})
	if _, err := s.Open(ctx, "agentA", nil, ""); err == nil {
		t.Fatal("open at cap should error")
	}
}

func TestSandboxCancelAndTerminal(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	// Cancel with no live connection is a no-op.
	s.Cancel(123)

	row := &models.Sandbox{Name: "grasp-sb-term", Purpose: "test", Status: "stopped"}
	db.Create(row)
	// container not running -> terminal errors.
	if _, err := s.OpenTerminal(ctx, row.ID); err == nil {
		t.Fatal("terminal on stopped container should error")
	}
	if _, err := s.OpenTerminal(ctx, 999); err == nil {
		t.Fatal("terminal on missing should error")
	}
}

func TestSandboxViewForRunNode(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	row := &models.Sandbox{Name: "grasp-sb-lookup", Purpose: "run", Status: "running", RunID: "runS", NodeID: "n1"}
	db.Create(row)
	ds.setStatus(row.Name, "running")

	v, err := s.SandboxViewForRunNode(ctx, "runS", "n1")
	if err != nil || v == nil || v.ID != row.ID || v.ContainerStatus != "running" {
		t.Fatalf("lookup: v=%+v err=%v", v, err)
	}

	ds.setStatus(row.Name, "exited")
	if _, err := s.SandboxViewForRunNode(ctx, "runS", "n1"); err == nil {
		t.Fatal("non-running container should 404")
	}
	if _, err := s.SandboxViewForRunNode(ctx, "no-run", "n1"); err == nil {
		t.Fatal("missing record should 404")
	}
}

func TestSandboxLogFallback(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	row := &models.Sandbox{Name: "grasp-sb-log", Purpose: "run", Status: "exited", RunID: "runL", NodeID: "n1"}
	db.Create(row)
	ds.setStatus(row.Name, "exited") // not running -> use archived fallback
	db.Create(&models.SandboxLog{Name: row.Name, RunID: "runL", NodeID: "n1", Content: "archived output"})

	content, live, err := s.NodeSandboxLog(ctx, "runL", "n1")
	if err != nil || live || content != "archived output" {
		t.Fatalf("nodelog: %q live=%v err=%v", content, live, err)
	}
	byID, live2, err := s.SandboxLogByID(ctx, row.ID)
	if err != nil || live2 || byID != "archived output" {
		t.Fatalf("logbyid: %q live=%v err=%v", byID, live2, err)
	}
	// Missing archive -> not found.
	if _, _, err := s.NodeSandboxLog(ctx, "no-run", ""); err == nil {
		t.Fatal("missing node log should error")
	}
	if _, _, err := s.SandboxLogByID(ctx, 999); err == nil {
		t.Fatal("missing sandbox log by id should error")
	}
}

// eventLogWSServer starts an httptest server whose /ws endpoint mimics the
// acp-bridge handshake, replying to {op:connect} with a connected frame
// carrying a small eventLog. Returns the server and its host/port.
func eventLogWSServer(t *testing.T) (*httptest.Server, string, int) {
	t.Helper()
	up := websocket.Upgrader{}
	frame := `{"op":"event","data":{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hello"}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "test", Path: "/"})
			return
		}
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			if strings.Contains(string(msg), "connect") {
				c.WriteJSON(map[string]any{
					"op":           "connected",
					"sessionId":    "s1",
					"eventLog":     []map[string]any{jsonRaw(frame)},
					"totalTurns":   1,
					"hasMoreTurns": false,
				})
			}
		}
	}))
	addr := srv.Listener.Addr().String()
	host, portStr, _ := strings.Cut(addr, ":")
	port, _ := strconv.Atoi(portStr)
	return srv, host, port
}

func jsonRaw(s string) map[string]any {
	// The bridge sends eventLog entries as JSON objects; embed the frame shape.
	return map[string]any{
		"op": "event",
		"data": map[string]any{
			"type": "session_update",
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": "hello"},
			},
		},
	}
}

func TestSandboxEventsAndLog(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	srv, host, port := eventLogWSServer(t)
	defer srv.Close()

	row := &models.Sandbox{Name: "grasp-sb-ev", Purpose: "test", Status: "running", Host: host, ACPPort: port, Token: "test-password"}
	db.Create(row)

	events, err := s.Events(ctx, row.ID)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected aggregated events")
	}
	frames, err := s.EventLog(ctx, row.ID)
	if err != nil || len(frames) == 0 {
		t.Fatalf("eventlog: %v n=%d", err, len(frames))
	}

	// acpHostPort error path: no host/port and container not running.
	dead := &models.Sandbox{Name: "grasp-sb-noconn", Purpose: "test", Status: "stopped"}
	db.Create(dead)
	if _, err := s.Events(ctx, dead.ID); err == nil {
		t.Fatal("events on dead sandbox should error")
	}
	if _, err := s.Events(ctx, 999); err == nil {
		t.Fatal("events on missing should error")
	}
}

// Running sandboxes prefer live gateway logs (including successful empty reads).
func TestSandboxLogByIDLive(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	row := &models.Sandbox{Name: "grasp-sb-livelog", Purpose: "test", Status: "running"}
	db.Create(row)
	ds.setStatus(row.Name, "running")
	ds.fg.SetLogs(row.Name, "live body")
	db.Create(&models.SandboxLog{Name: row.Name, Content: "archived body"})
	content, live, err := s.SandboxLogByID(ctx, row.ID)
	if err != nil || !live || content != "live body" {
		t.Fatalf("log: %q live=%v err=%v (want live body, live=true)", content, live, err)
	}

	// Live empty read must surface as found/live, not fall through to archive.
	empty := &models.Sandbox{Name: "grasp-sb-liveempty", Purpose: "test", Status: "running"}
	db.Create(empty)
	ds.setStatus(empty.Name, "running")
	ds.fg.SetLogs(empty.Name, "")
	db.Create(&models.SandboxLog{Name: empty.Name, Content: "archived empty-fallback"})
	cEmpty, liveEmpty, err := s.SandboxLogByID(ctx, empty.ID)
	if err != nil || !liveEmpty || cEmpty != "" {
		t.Fatalf("live empty: %q live=%v err=%v", cEmpty, liveEmpty, err)
	}

	run := &models.Sandbox{Name: "grasp-sb-runlive", Purpose: "run", Status: "running", RunID: "rl", NodeID: "n"}
	db.Create(run)
	ds.setStatus(run.Name, "running")
	ds.fg.SetLogs(run.Name, "live run body")
	db.Create(&models.SandboxLog{Name: run.Name, RunID: "rl", NodeID: "n", Content: "archived run body"})
	c2, live2, err := s.NodeSandboxLog(ctx, "rl", "n")
	if err != nil || !live2 || c2 != "live run body" {
		t.Fatalf("node log: %q live=%v err=%v (want live, live=true)", c2, live2, err)
	}

	// Live read failure must propagate (not disguise as no-source / archive).
	fail := &models.Sandbox{Name: "grasp-sb-livefail", Purpose: "run", Status: "running", RunID: "rf", NodeID: "n"}
	db.Create(fail)
	ds.setStatus(fail.Name, "running")
	ds.fg.FailLogs = true
	if _, _, err := s.NodeSandboxLog(ctx, "rf", "n"); err == nil {
		t.Fatal("live logs failure should error")
	}
	ds.fg.FailLogs = false
}

func TestSandboxFindReusable(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	// A live running test sandbox for agentA is reused by Open (no new create).
	live := &models.Sandbox{Name: "grasp-sb-reuse", Purpose: "test", Profile: "agentA", Status: "running", RepoURL: ""}
	db.Create(live)
	ds.setStatus(live.Name, "running")
	got, err := s.Open(ctx, "agentA", nil, "")
	if err != nil {
		t.Fatalf("open reuse: %v", err)
	}
	if got.ID != live.ID {
		t.Fatalf("expected reuse of id %d, got %d", live.ID, got.ID)
	}

	// With only a "creating" row, findReusable falls back to it.
	db.Where("1 = 1").Delete(&models.Sandbox{})
	creating := &models.Sandbox{Name: "grasp-sb-creating", Purpose: "test", Profile: "agentA", Status: "creating", RepoURL: ""}
	db.Create(creating)
	if r := s.findReusable(ctx, "agentA", ""); r == nil || r.ID != creating.ID {
		t.Fatalf("expected creating fallback, got %+v", r)
	}
	// A running row whose container is actually gone is NOT reused.
	db.Where("1 = 1").Delete(&models.Sandbox{})
	stale := &models.Sandbox{Name: "grasp-sb-stale", Purpose: "test", Profile: "agentA", Status: "running", RepoURL: ""}
	db.Create(stale) // no ds.setStatus -> docker reports not_found
	if r := s.findReusable(ctx, "agentA", ""); r != nil {
		t.Fatalf("stale running row should not be reused, got %+v", r)
	}
}

func TestSandboxFindReusableProjectScope(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	liveA := &models.Sandbox{
		Name: "grasp-sb-proj-a", Purpose: "test", Profile: "agentA",
		Status: "running", RepoURL: "", ProjectID: "proj-a",
	}
	liveB := &models.Sandbox{
		Name: "grasp-sb-proj-b", Purpose: "test", Profile: "agentA",
		Status: "running", RepoURL: "", ProjectID: "proj-b",
	}
	db.Create(liveA)
	db.Create(liveB)
	ds.setStatus(liveA.Name, "running")
	ds.setStatus(liveB.Name, "running")

	if r := s.findReusable(ctx, "agentA", "proj-a"); r == nil || r.ID != liveA.ID {
		t.Fatalf("expected proj-a sandbox, got %+v", r)
	}
	if r := s.findReusable(ctx, "agentA", "proj-b"); r == nil || r.ID != liveB.ID {
		t.Fatalf("expected proj-b sandbox, got %+v", r)
	}
	if r := s.findReusable(ctx, "agentA", "proj-c"); r != nil {
		t.Fatalf("unexpected reuse for unknown project: %+v", r)
	}
}

func TestBuildTestSandboxSpecsSchedulerInject(t *testing.T) {
	config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "http://spa.example.com"}})
	defer config.StoreConfig(nil)
	db := newTestDB(t)
	s := newSandboxService(t, db, &dockerState{})
	s.mcpEndpoint = "http://spa.example.com"
	registered := false
	unregistered := false
	s.SetTestSchedulerHooks(TestSchedulerHooks{
		Register: func(projectID, profile, runID, token string) {
			if projectID != "proj-x" || profile != "agentA" || runID != "run1" || token != "tok" {
				t.Fatalf("register mismatch: %q %q %q %q", projectID, profile, runID, token)
			}
			registered = true
		},
		Unregister: func(token string) {
			if token != "tok" {
				t.Fatalf("unregister token = %q", token)
			}
			unregistered = true
		},
	})
	agent := Agent{
		MCP: []MCPServer{{Name: "task-scheduler", URL: "${GRASP_SCHEDULER_URL}"}},
	}
	vars := s.testMcpVars("run1", "tok", "proj-x", "agentA")
	specs := s.buildTestSandboxSpecs("proj-x", "agentA", "run1", "tok", agent, vars)
	if !registered {
		t.Fatal("expected scheduler register hook")
	}
	if len(specs) != 1 || specs[0].Name != TaskSchedulerMCP {
		t.Fatalf("specs = %+v", specs)
	}
	if vars["GRASP_SCHEDULER_URL"] == "" || vars["GRASP_SCHEDULER_TOKEN"] != "tok" {
		t.Fatalf("vars = %+v", vars)
	}
	s.unregisterTestScheduler("proj-x", "tok")
	if !unregistered {
		t.Fatal("expected scheduler unregister hook")
	}
	empty := s.buildTestSandboxSpecs("", "agentA", "run1", "tok", agent, s.testMcpVars("run1", "tok", "", "agentA"))
	if len(empty) != 0 {
		t.Fatalf("no project should not inject scheduler, got %+v", empty)
	}
}

func TestSandboxOpenReposSkipReuse(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	live := &models.Sandbox{Name: "grasp-sb-reuse-repos", Purpose: "test", Profile: "agentA", Status: "running", RepoURL: ""}
	db.Create(live)
	ds.setStatus(live.Name, "running")

	repos := []sandbox.RepoSpec{{Name: "web", URL: "https://h/web.git"}}
	got, err := s.Open(ctx, "agentA", repos, "")
	if err != nil {
		t.Fatalf("open with repos: %v", err)
	}
	if got.ID == live.ID {
		t.Fatalf("should not reuse sandbox when repos are configured")
	}
	if got.RepoURL != "https://h/web.git" {
		t.Fatalf("RepoURL = %q, want first repo URL", got.RepoURL)
	}

	// Empty repos list still reuses.
	got2, err := s.Open(ctx, "agentA", nil, "")
	if err != nil {
		t.Fatalf("open empty: %v", err)
	}
	if got2.ID != live.ID {
		t.Fatalf("empty repos should reuse id %d, got %d", live.ID, got2.ID)
	}
}

func TestFirstTestRepoURL(t *testing.T) {
	if firstTestRepoURL(nil) != "" {
		t.Fatal("nil repos")
	}
	if firstTestRepoURL([]sandbox.RepoSpec{}) != "" {
		t.Fatal("empty repos")
	}
	if got := firstTestRepoURL([]sandbox.RepoSpec{{URL: "https://a.git"}, {URL: "https://b.git"}}); got != "https://a.git" {
		t.Fatalf("first = %q", got)
	}
}

func TestSandboxCleanupAndSweepBusySkip(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	// A busy run sandbox is skipped by CleanupIdle; an idle test row is destroyed.
	busyName := "grasp-sb-busy2"
	s.RegisterRunSandbox(runtimeInfo(busyName)) // marks runActive (busy)
	db.Create(&models.Sandbox{Name: "grasp-sb-idle", Purpose: "test", Status: "running"})
	destroyed, skipped := s.CleanupIdle(ctx)
	if destroyed < 1 || skipped < 1 {
		t.Fatalf("cleanup destroyed=%d skipped=%d (want >=1 each)", destroyed, skipped)
	}

	// sweepOnce skips a busy row even past its TTL deadline.
	past := time.Now().Add(-time.Hour)
	db.Model(&models.Sandbox{}).Where("name = ?", busyName).Update("destroy_at", &past)
	s.sweepOnce(ctx)
	var stillThere int64
	db.Model(&models.Sandbox{}).Where("name = ?", busyName).Count(&stillThere)
	if stillThere != 1 {
		t.Fatalf("busy row should survive sweep, count=%d", stillThere)
	}
}

func TestSandboxAcpHostPortAttach(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	// A row with no cached host/port but a running container -> acpHostPort
	// attaches to derive the port (then FetchEventLog fails on the dead port,
	// which still exercises the attach branch).
	row := &models.Sandbox{Name: "grasp-sb-attach", Purpose: "test", Status: "running"}
	db.Create(row)
	ds.setStatus(row.Name, "running")
	if _, err := s.Events(ctx, row.ID); err == nil {
		t.Log("attach succeeded and event fetch unexpectedly ok") // either way the attach branch ran
	}
}

func TestWorkflowDeleteError(t *testing.T) {
	db := newTestDB(t)
	wf := NewWorkflowService(db)
	sqlDB, _ := db.DB()
	sqlDB.Close()
	if err := wf.Delete("x"); err == nil {
		t.Error("delete on closed db should error")
	}
}

func TestSandboxChatErrors(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	sink := func(json.RawMessage) {}
	// Missing sandbox.
	if err := s.Chat(ctx, 999, "hi", nil, sink); err == nil {
		t.Fatal("chat missing should error")
	}
	// Container not running -> ensureConnected error.
	row := &models.Sandbox{Name: "grasp-sb-chat", Purpose: "test", Status: "stopped"}
	db.Create(row)
	if err := s.Chat(ctx, row.ID, "hi", nil, sink); err == nil {
		t.Fatal("chat on stopped container should error")
	}
}

func TestSandboxMcpVars(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	vars := s.mcpVars("run1", "tok")
	if vars["GRASP_RUN_ID"] != "run1" || vars["GRASP_ARTIFACT_TOKEN"] != "tok" {
		t.Fatalf("mcpVars: %+v", vars)
	}
	if !strings.Contains(vars["GRASP_ARTIFACT_URL"], "run1") {
		t.Fatalf("artifact url: %s", vars["GRASP_ARTIFACT_URL"])
	}
}

func TestSandboxMcpVarsUsesLiveConfigPassthrough(t *testing.T) {
	prev := config.GetConfig()
	t.Cleanup(func() { config.StoreConfig(prev) })
	config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "http://api.example.com"}})

	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	vars := s.mcpVars("run-spa", "tok")
	want := "http://api.example.com/mcp/runs/run-spa"
	if vars["GRASP_ARTIFACT_URL"] != want {
		t.Fatalf("GRASP_ARTIFACT_URL = %q, want %q", vars["GRASP_ARTIFACT_URL"], want)
	}
}

func TestSandboxMcpVarsUsesOptionsFallbackPassthrough(t *testing.T) {
	prev := config.GetConfig()
	t.Cleanup(func() { config.StoreConfig(prev) })
	config.StoreConfig(nil) // force Options fallback path

	db := newTestDB(t)
	ds := &dockerState{}
	s := newSandboxService(t, db, ds)
	s.mcpEndpoint = "http://api.example.com"
	vars := s.mcpVars("run-opt", "tok")
	want := "http://api.example.com/mcp/runs/run-opt"
	if vars["GRASP_ARTIFACT_URL"] != want {
		t.Fatalf("GRASP_ARTIFACT_URL = %q, want %q", vars["GRASP_ARTIFACT_URL"], want)
	}
}

// fakeACPServer starts an httptest server whose /ws endpoint speaks enough of
// the ACP protocol for Connect + ChatStream: it acks connect with a connected
// frame and answers each chat with one event + prompt_done.
func fakeACPServer(t *testing.T) (*httptest.Server, int) {
	t.Helper()
	up := websocket.Upgrader{}
	mux := http.NewServeMux()
	// Mirror the real bridge's unified-auth login: hand out a session cookie so
	// the platform's WaitForACPReady / ACP client can authenticate before /ws.
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "test-cookie", Path: "/"})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"next":"/"}`))
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			var m struct {
				Op   string `json:"op"`
				OpID string `json:"opId"`
			}
			json.Unmarshal(msg, &m)
			switch m.Op {
			case "connect":
				c.WriteJSON(map[string]any{"op": "connected", "sessionId": "sess-1"})
			case "chat":
				c.WriteJSON(map[string]any{"op": "event", "opId": m.OpID, "data": map[string]any{
					"type": "session_update",
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"content":       map[string]any{"type": "text", "text": "hi"},
					},
				}})
				c.WriteJSON(map[string]any{"op": "event", "opId": m.OpID, "data": map[string]any{"type": "prompt_done"}})
			}
		}
	})
	srv := httptest.NewServer(mux)
	_, portStr, _ := strings.Cut(srv.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(portStr)
	return srv, port
}

func TestSandboxOpenLifecycle(t *testing.T) {
	db := newTestDB(t)
	acp, port := fakeACPServer(t)
	defer acp.Close()
	ds := &dockerState{acpPort: port}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	// Open creates a "creating" row and launches the background container.
	row, err := s.Open(ctx, "agentA", nil, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Poll until startContainer flips the row to running.
	deadline := time.Now().Add(6 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		var r models.Sandbox
		db.First(&r, row.ID)
		status = r.Status
		if status == "running" || status == "error" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "running" {
		t.Fatalf("sandbox did not reach running: %q", status)
	}

	// Chat over the live connection succeeds (event + prompt_done).
	var got strings.Builder
	if err := s.Chat(ctx, row.ID, "hello", nil, func(raw json.RawMessage) {
		got.Write(raw)
	}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got.Len() == 0 {
		t.Fatal("expected streamed acp events")
	}

	// Cancel then teardown via Destroy exercises teardownLive.
	s.Cancel(row.ID)
	if err := s.Destroy(ctx, row.ID); err != nil {
		t.Fatalf("destroy: %v", err)
	}
}

func TestSandboxOpenWithEffectiveKeepsNormalizedOpenCodeModel(t *testing.T) {
	db := newTestDB(t)
	acp, port := fakeACPServer(t)
	defer acp.Close()
	ds := &dockerState{acpPort: port}
	s := newSandboxService(t, db, ds)

	effective := Agent{
		Name:       "agentA",
		AcpBackend: "opencode",
		Env: map[string]string{
			"GRASP_OPENCODE_PROVIDER": "tencent-tokenhub",
			"ACP_BRIDGE_MODEL":        "deepseek/deepseek-flash",
			"BROWSER_MCP":             "1",
		},
		Layout: AgentLayout{
			ConfigRoot:   "/root/.config/opencode",
			WorkspaceDir: "/root/workspace",
		},
	}
	row, err := s.OpenWithEffective(
		context.Background(), "agentA", "proj-1", nil, effective, t.TempDir(),
	)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		var current models.Sandbox
		if err := db.First(&current, row.ID).Error; err == nil &&
			(current.Status == "running" || current.Status == "error") {
			if current.Status != "running" {
				t.Fatalf("sandbox status=%s error=%s", current.Status, current.Error)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	env := ds.fg.LastCreateEnv()
	if got := env["ACP_BRIDGE_MODEL"]; got != "tencent-tokenhub/deepseek/deepseek-flash" {
		t.Fatalf("ACP_BRIDGE_MODEL=%q", got)
	}
}

func TestSandboxChatReconnect(t *testing.T) {
	db := newTestDB(t)
	acp, port := fakeACPServer(t)
	defer acp.Close()
	ds := &dockerState{acpPort: port}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	// A running row with no in-memory live connection forces ensureConnected to
	// re-attach (Attach -> ACP Connect) lazily.
	name := "grasp-sb-reconn"
	ds.setStatus(name, "running")
	row := &models.Sandbox{Name: name, Profile: "agentA", Purpose: "test", Status: "running", Token: "test-password"}
	db.Create(row)

	if err := s.Chat(ctx, row.ID, "hi", nil, func(json.RawMessage) {}); err != nil {
		t.Fatalf("chat reconnect: %v", err)
	}
	// A second chat reuses the now-live connection (IsConnected path).
	if err := s.Chat(ctx, row.ID, "again", nil, func(json.RawMessage) {}); err != nil {
		t.Fatalf("second chat: %v", err)
	}
}

func TestSandboxOpenCreateFailure(t *testing.T) {
	db := newTestDB(t)
	ds := &dockerState{failRun: true}
	s := newSandboxService(t, db, ds)
	ctx := context.Background()

	row, err := s.Open(ctx, "agentA", nil, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// startContainer should flip the row to "error" when docker run fails.
	deadline := time.Now().Add(4 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		var r models.Sandbox
		db.First(&r, row.ID)
		status = r.Status
		if status == "error" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "error" {
		t.Fatalf("expected error status, got %q", status)
	}
}

func TestSandboxHelpers(t *testing.T) {
	vars := map[string]string{"A": "1", "B": "2"}
	if substTemplate("x-${A}-${B}", vars) != "x-1-2" {
		t.Fatal("substTemplate")
	}
	if m := substTemplateMap(map[string]string{"k": "${A}"}, vars); m["k"] != "1" {
		t.Fatal("substTemplateMap")
	}
	if substTemplateMap(nil, vars) != nil {
		t.Fatal("substTemplateMap nil")
	}
	if sl := substTemplateSlice([]string{"${A}", "z"}, vars); sl[0] != "1" || sl[1] != "z" {
		t.Fatal("substTemplateSlice")
	}
	if substTemplateSlice(nil, vars) != nil {
		t.Fatal("substTemplateSlice nil")
	}

	specs := resolveAgentMCP([]MCPServer{
		{Name: "artifact-store", URL: "${A}"},
		{Name: "cmd", Command: "run ${B}", Args: []string{"${A}"}, Env: map[string]string{"E": "${B}"}},
		{Name: "empty"},      // no url/cmd -> dropped
		{Name: "", URL: "x"}, // no name -> dropped
	}, vars)
	if len(specs) != 2 {
		t.Fatalf("resolveAgentMCP got %d specs", len(specs))
	}
	if resolveAgentMCP(nil, vars) != nil {
		t.Fatal("resolveAgentMCP nil")
	}
	if !hasArtifactStoreSpec(specs) {
		t.Fatal("hasArtifactStoreSpec")
	}
	if hasArtifactStoreSpec(nil) {
		t.Fatal("hasArtifactStoreSpec nil should be false")
	}

	js := mcpServersJSON(specs)
	if len(js) == 0 || !strings.Contains(string(js), "artifact-store") {
		t.Fatalf("mcpServersJSON: %s", js)
	}
	if mcpServersJSON(nil) != nil {
		t.Fatal("mcpServersJSON nil")
	}

	if truncErr(nil) != "" {
		t.Fatal("truncErr nil")
	}
	long := strings.Repeat("e", 600)
	if len(truncErr(fmt.Errorf("%s", long))) != 500 {
		t.Fatal("truncErr should cap at 500")
	}

	if randID() == "" {
		t.Fatal("randID empty")
	}
}

func TestResolveAgentMCPSubstitutesMergedEnv(t *testing.T) {
	base := map[string]string{
		"GRASP_ARTIFACT_URL":   "http://art",
		"GRASP_ARTIFACT_TOKEN": "tok",
	}
	vars := runtime.MergeEnvIntoTemplateVars(base, map[string]string{
		"LOG_CENTER_TOKEN":     "secret-from-shared",
		"GRASP_ARTIFACT_TOKEN": "evil",
	})
	specs := resolveAgentMCP([]MCPServer{
		{Name: "server-log", URL: "https://logs.example/mcp", Headers: map[string]string{"Authorization": "Bearer ${LOG_CENTER_TOKEN}"}},
		{Name: "artifact-store", URL: "${GRASP_ARTIFACT_URL}", Headers: map[string]string{"Authorization": "Bearer ${GRASP_ARTIFACT_TOKEN}"}},
	}, vars)
	if len(specs) != 2 {
		t.Fatalf("specs=%d", len(specs))
	}
	byName := map[string]sandbox.MCPServerSpec{}
	for _, sp := range specs {
		byName[sp.Name] = sp
	}
	if got := byName["server-log"].Headers["Authorization"]; got != "Bearer secret-from-shared" {
		t.Fatalf("server-log Authorization=%q", got)
	}
	if got := byName["artifact-store"].Headers["Authorization"]; got != "Bearer tok" {
		t.Fatalf("artifact-store Authorization=%q", got)
	}
}

func TestSandboxServiceSettersAndShutdownAll(t *testing.T) {
	db := newTestDB(t)
	svc := newSandboxService(t, db, &dockerState{})
	svc.SetTTLs(2*time.Minute, 3*time.Minute)
	if svc.RunTTL() != 2*time.Minute || svc.TTL() != 3*time.Minute {
		t.Fatalf("ttls: run=%v test=%v", svc.RunTTL(), svc.TTL())
	}
	svc.SetMaxTestSandboxes(5)
	if svc.MaxTestSandboxes() != 5 {
		t.Fatalf("max: %d", svc.MaxTestSandboxes())
	}
	db.Create(&models.Sandbox{Name: "grasp-sb-shutdown", Purpose: "test", Status: "stopped"})
	if n := svc.ShutdownAllTestSandboxes(context.Background(), true); n != 1 {
		t.Fatalf("shutdown: %d", n)
	}
}
