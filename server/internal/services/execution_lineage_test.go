package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

func serviceExecutionController(t *testing.T, db *gorm.DB) (*executionlineage.Controller, *executionlineage.SQLAuthority, string) {
	t.Helper()
	ctx := t.Context()
	authority, err := executionlineage.OpenSQL(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal(err)
	}
	ticket, err := authority.Initialize(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := executionlineage.EnsureRecoverySchema(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { _, err := authority.QuarantineTx(tx, owner, ticket); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.Activate(ctx, db, owner, ticket); err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Leave(ctx, db, owner); err != nil {
		t.Fatal(err)
	}
	controller, err := executionlineage.NewLocalController(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	return controller, authority, owner
}

func TestServiceLineageArtifactPrepareDoesNotHoldSQLAndCannotPublishAfterRestore(t *testing.T) {
	db := newTestDB(t)
	controller, authority, owner := serviceExecutionController(t, db)
	if err := db.Table("runs").Create(map[string]any{"id": "new-artifact-run", "status": "running"}).Error; err != nil {
		t.Fatal(err)
	}
	files, err := filestore.FromDB(db)
	if err != nil {
		t.Fatal(err)
	}
	objects := &journalPublicationObjects{journalViewObjects: &journalViewObjects{objects: map[string]journalViewObject{}}}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	arts := NewArtifactService(db)
	arts.SetExecutionController(controller)
	id, err := arts.Save("new-artifact-run", "writer", "kept.txt", "text", "previous reply")
	if err != nil || id == "" {
		t.Fatal("current explicit run could not publish", err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	objects.beforePut = func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	result := make(chan error, 1)
	go func() {
		_, err := arts.Save("new-artifact-run", "writer", "kept.txt", "text", "late native result")
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("body preparation never started")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	owner = uuid.NewString()
	if err := storageguard.Enter(ctx, db, owner, nil); err != nil {
		t.Fatal("artifact PUT retained a SQL lock", err)
	}
	if _, err := authority.BeginRestore(ctx, owner, strings.Repeat("f", 64)); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	if err := <-result; !errors.Is(err, executionlineage.ErrInactive) {
		t.Fatal("revoked worker published its late reply", err)
	}
	if content, ok := arts.Get("new-artifact-run", "kept.txt"); !ok || content != "previous reply" {
		t.Fatal("execution quarantine made historical content unreadable or replaced it")
	}
}

func TestServiceLineageWorkspaceChecksParentsAndFencesExpiryRelease(t *testing.T) {
	db := newTestDB(t)
	if err := db.Table("solo_sessions").Create(map[string]any{"id": "restored-solo", "status": "restarting"}).Error; err != nil {
		t.Fatal(err)
	}
	controller, authority, owner := serviceExecutionController(t, db)
	sbx := newSandboxService(t, db, &dockerState{})
	sbx.execution = controller
	resource := models.Sandbox{Name: "new-resource", Purpose: SandboxPurposeSolo, ThreadID: "new-solo", Status: "running"}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	options, err := WithWorkspaceJobExecution(WorkspaceJobOptions{
		Authorize: func(context.Context, *gorm.DB, models.WorkspaceSnapshotJob) error { return nil }, // Business scope is separately exercised by the job tests.
		Remote:    func(context.Context, uint) (WorkspaceJobRemote, error) { return &jobRemote{}, nil },
		Validate:  map[string]func(json.RawMessage) error{"control_probe": func(json.RawMessage) error { return nil }},
	}, controller)
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewWorkspaceSnapshotJobs(db, sbx, options)
	input := WorkspaceJobInput{ClientRequestID: "new-explicit-job", ScopeKind: "solo", ScopeID: "new-solo", SandboxID: resource.ID, Kind: "control_probe", Duration: time.Minute}
	row, err := jobs.Admit(t.Context(), "alice", input)
	if err != nil {
		t.Fatal("active explicit workspace admission failed", err)
	}
	_, lease, err := jobs.claim(t.Context(), row.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Table("runs").Create(map[string]any{"id": "new-child-of-restored-solo", "status": "running", "origin_solo_id": "restored-solo"}).Error; err != nil {
		t.Fatal(err)
	}
	input.ClientRequestID, input.ScopeKind, input.ScopeID = "new-but-quarantined-parent", "run", "new-child-of-restored-solo"
	if _, err := jobs.Admit(t.Context(), "alice", input); !errors.Is(err, executionlineage.ErrQuarantined) {
		t.Fatal("a new child bypassed its restored Solo parent", err)
	}
	if err := db.Model(&models.WorkspaceSnapshotJob{}).Where("id = ?", row.ID).Update("deadline", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	owner = uuid.NewString()
	if err := storageguard.Enter(t.Context(), db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.BeginRestore(t.Context(), owner, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	jobs.expire(t.Context(), row.Lineage)
	jobs.release(lease)
	var after models.WorkspaceSnapshotJob
	if err := db.Take(&after, "id = ?", row.ID).Error; err != nil || after.State != "queued" || after.Owner != lease.owner {
		t.Fatal("old worker mutated restored job expiry/lease", after.State, after.Owner, err)
	}
	if _, err := jobs.renew(t.Context(), lease, time.Time{}); err == nil {
		t.Fatal("revoked workspace worker renewed its lease")
	}
	if _, err := jobs.Get(t.Context(), "alice", row.ID); err != nil {
		t.Fatal("execution revocation prevented authorized history inspection", err)
	}
}

func TestServiceLineageStartupDoesNotAdoptOrRewriteQuarantinedSandbox(t *testing.T) {
	db := newTestDB(t)
	state := &dockerState{}
	sbx := newSandboxService(t, db, state)
	row := models.Sandbox{Name: "restored-resource", Purpose: "test", Status: "running", Error: "retained historical attribution"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	state.setStatus(row.Name, "running")
	controller, _, _ := serviceExecutionController(t, db)
	sbx.execution = controller
	sbx.ReconcileOnStartup(t.Context(), nil)
	var after models.Sandbox
	if err := db.Take(&after, row.ID).Error; err != nil || after.Status != row.Status || after.Error != row.Error || after.Generation != row.Generation || after.Host != "" || after.DestroyAt != nil {
		t.Fatal("startup reconciler adopted/replaced a restored execution resource", err)
	}
}

func TestServiceLineageStudioPhysicalLeaseCancelsFlyingNativeTurn(t *testing.T) {
	started, acknowledged := make(chan string, 1), make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "agentchat_session", Value: "test-cookie", Path: "/"})
		_, _ = w.Write([]byte(`{"ok":true,"next":"/"}`))
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		upgrade := websocket.Upgrader{}
		conn, err := upgrade.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var frame struct {
				Op string `json:"op"`
				ID string `json:"opId"`
			}
			if conn.ReadJSON(&frame) != nil {
				return
			}
			switch frame.Op {
			case "connect":
				if conn.WriteJSON(map[string]any{"op": "connected", "sessionId": "native-lineage"}) != nil {
					return
				}
			case "chat":
				started <- frame.ID
			case "cancel":
				acknowledged <- frame.ID
				if conn.WriteJSON(map[string]any{"op": "event", "opId": frame.ID, "data": map[string]any{"type": "prompt_done", "stopReason": "cancelled"}}) != nil {
					return
				}
			}
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	_, portText, _ := strings.Cut(server.Listener.Addr().String(), ":")
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	db := newTestDB(t)
	controller, authority, owner := serviceExecutionController(t, db)
	sbx := newSandboxService(t, db, &dockerState{acpPort: port})
	sbx.execution, sbx.operationRenewInterval = controller, 5*time.Millisecond
	resource, err := sbx.Open(t.Context(), "agentA", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sbx.WaitReady(t.Context(), resource.ID, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sbx.mu.Lock()
		live := sbx.live[resource.ID]
		sbx.mu.Unlock()
		if live != nil && live.acp != nil {
			live.acp.Close()
		}
	})
	result := make(chan error, 1)
	go func() {
		_, _, err := sbx.ChatWithTimeout(t.Context(), resource.ID, "a flying native operation", nil, 0, nil)
		result <- err
	}()
	var original string
	select {
	case original = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("native Studio turn never started")
	}
	if original == "" {
		t.Fatal("native turn was missing its operation identity")
	}
	owner = uuid.NewString()
	if err := storageguard.Enter(t.Context(), db, owner, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := authority.BeginRestore(t.Context(), owner, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, errSandboxOperationLost) || !errors.Is(err, executionlineage.ErrInactive) {
			t.Fatal("Studio did not stop at its revoked physical lease", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Studio native turn kept running after restore invalidated its lease")
	}
	select {
	case stopped := <-acknowledged:
		if stopped != original {
			t.Fatal("cancel targeted a different operation", stopped, original)
		}
	default:
		t.Fatal("native original-operation cancellation was not acknowledged")
	}
	var after models.Sandbox
	if err := db.Take(&after, resource.ID).Error; err != nil || after.OperationOwner == "" || after.DestroyAt != nil {
		t.Fatal("old completion/release mutated restored lease evidence", err)
	}
}
