package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/filestore"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/objectstore"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
)

func workspaceCheckpointFixture(t *testing.T, enabled bool) (*Engine, *execCtx, *models.Node, models.StateRun) {
	t.Helper()
	db, _, controller := executionEngineFixture(t, nil)
	checkpoints, err := services.NewWorkflowWorkspaceCheckpoints(db, services.WorkflowCheckpointOptions{Enabled: enabled, Execution: controller, AccountExists: func(actor string) bool { return actor == "alice" }, ReadScope: func(ctx context.Context, db *gorm.DB, actor, kind, id string) error {
		if actor != "alice" || kind != "run" {
			return gorm.ErrRecordNotFound
		}
		var count int64
		if err := db.WithContext(ctx).Table("runs").Where("id = ?", id).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	}, ResolveBinding: func(context.Context, string, string, string, uint) (services.WorkspaceActiveBinding, error) {
		return services.WorkspaceActiveBinding{}, services.ErrWorkspaceJobUnsupported
	}, Capture: func(context.Context, string, services.WorkspaceCaptureInput) (services.WorkspaceSnapshotMetadata, error) {
		return services.WorkspaceSnapshotMetadata{}, services.ErrWorkspaceJobUnsupported
	}, Reconcile: func(context.Context, string, string, string, string) (services.WorkspaceSnapshotMetadata, error) {
		return services.WorkspaceSnapshotMetadata{}, services.ErrWorkspaceJobUnsupported
	}})
	if err != nil {
		t.Fatal(err)
	}
	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	e := newEngine(db, &fakeProvider{host: host}, host, arts, 5, controller, checkpoints)
	graph := models.Graph{Nodes: []models.Node{{ID: "agent", Type: "agent", Caps: &models.AgentCapabilities{}}, {ID: "next", Type: "output"}}, Edges: []models.Edge{{ID: "edge", Source: "agent", Target: "next"}}}
	run := models.Run{ID: "checkpoint-run", WorkflowID: "workflow", Status: "running", WorkspaceActorID: "alice", Graph: graph, Checkpoints: map[string]map[string]any{}, Trace: []models.TraceEntry{}}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state := models.StateRun{RunID: run.ID, NodeID: "agent", NodeType: "agent", Iteration: 1, Attempt: 0, Status: "running", StartedAt: &now, VarsBefore: map[string]any{}}
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	c := &execCtx{run: &run, graph: graph, vars: map[string]any{}, nodeOutputs: map[string]map[string]any{}, iter: map[string]int{"agent": 1}, autoRetries: map[string]int{}}
	return e, c, c.graph.FindNode("agent"), state
}

func TestWorkspaceCheckpointDisabledSealsAndRoutesOnlyOnceFromImmutableOutcome(t *testing.T) {
	e, c, node, state := workspaceCheckpointFixture(t, false)
	ctx := context.Background()
	if err := e.sealWorkspaceCheckpoint(ctx, c, node, nodeOutcome{status: "completed", outputMd: "original response", outputs: map[string]any{"answer": "original"}}, finishOpts{trigger: triggerAuto}); err != nil {
		t.Fatal(err)
	}
	row, err := e.workspaceCheckpoints.Get(ctx, services.WorkflowCheckpointID(c.run.ID, state.ID, 0))
	if err != nil {
		t.Fatal(err)
	}
	if row.RouteState != "sealed" || row.FileState != "skipped" || row.Reason != "workspace_operations_disabled" || !filestore.IsRef(row.OutcomeRef) {
		t.Fatal("missing explicit no-snapshot policy", row)
	}
	var run models.Run
	if err := e.db.First(&run, "id = ?", c.run.ID).Error; err != nil || run.Status != "paused" {
		t.Fatal(run.Status, err)
	}
	// Mutating the in-memory outcome after sealing must not change recovery.
	c.graph.Edges = nil
	if err := e.advanceWorkspaceCheckpoint(ctx, row); err != nil {
		t.Fatal(err)
	}
	if err := e.db.First(&run, "id = ?", c.run.ID).Error; err != nil || run.Status != "queued" {
		t.Fatal(run.Status, err)
	}
	if got, ok := e.admissionFromNode(run); !ok || got != "next" {
		t.Fatal("sealed successor lost", got, ok)
	}
	var got models.StateRun
	if err := e.db.First(&got, state.ID).Error; err != nil || got.OutputMd != "original response" || got.Status != "completed" {
		t.Fatal(got, err)
	}
	var before, after int64
	e.db.Model(&models.WorkflowConversationEvent{}).Where("run_id = ?", c.run.ID).Count(&before)
	if err := e.advanceWorkspaceCheckpoint(ctx, row); err == nil {
		t.Fatal("old checkpoint applied twice")
	}
	e.db.Model(&models.WorkflowConversationEvent{}).Where("run_id = ?", c.run.ID).Count(&after)
	if before != after {
		t.Fatal("duplicate outbox generation", before, after)
	}
}

func TestWorkspaceCheckpointCurrentControlAndVariablesFenceFrozenRoute(t *testing.T) {
	for _, mutation := range []string{"control", "graph", "variable"} {
		t.Run(mutation, func(t *testing.T) {
			e, c, node, state := workspaceCheckpointFixture(t, false)
			if mutation == "variable" {
				c.vars["idea"] = "before"
				if err := e.db.Create(&models.RunVariable{RunID: c.run.ID, Name: "idea", Value: "before"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := e.sealWorkspaceCheckpoint(t.Context(), c, node, nodeOutcome{status: "completed", outputs: map[string]any{}}, finishOpts{trigger: triggerConfirm}); err != nil {
				t.Fatal(err)
			}
			row, err := e.workspaceCheckpoints.Get(t.Context(), services.WorkflowCheckpointID(c.run.ID, state.ID, 0))
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "control":
				e.db.Table("runs").Where("id = ?", c.run.ID).Update("control_version", 1)
			case "graph":
				e.db.Table("runs").Where("id = ?", c.run.ID).Update("graph", `{"nodes":[],"edges":[]}`)
			case "variable":
				e.db.Model(&models.RunVariable{}).Where("run_id = ?", c.run.ID).Update("value", `"new"`)
			}
			if err := e.advanceWorkspaceCheckpoint(t.Context(), row); !errors.Is(err, services.ErrWorkspaceJobConflict) {
				t.Fatal("changed source applied frozen route", err)
			}
			var got models.StateRun
			e.db.Select("status").First(&got, state.ID)
			if got.Status != "running" {
				t.Fatal("partial StateRun commit", got.Status)
			}
		})
	}
}

type checkpointBlockedObjects struct {
	objectstore.Store
	once             sync.Once
	entered, release chan struct{}
	puts             atomic.Int32
}

func (b *checkpointBlockedObjects) Put(ctx context.Context, _ string, _ io.ReadSeeker, _ objectstore.Info) error {
	b.puts.Add(1)
	b.once.Do(func() { close(b.entered) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.release:
		return errors.New("object backend unavailable")
	}
}

func TestWorkspaceCheckpointSlowOutcomeUploadRetainsDiskBarrierAndNoSQLLock(t *testing.T) {
	e, c, node, state := workspaceCheckpointFixture(t, false)
	files, err := filestore.FromDB(e.db)
	if err != nil {
		t.Fatal(err)
	}
	objects := &checkpointBlockedObjects{entered: make(chan struct{}), release: make(chan struct{})}
	if err := files.SetObjectStore(objects); err != nil {
		t.Fatal(err)
	}
	pool, _ := e.db.DB()
	pool.SetMaxOpenConns(1)
	finished := make(chan error, 1)
	go func() {
		finished <- e.sealWorkspaceCheckpoint(context.Background(), c, node, nodeOutcome{status: "completed", outputMd: "response"}, finishOpts{trigger: triggerAuto})
	}()
	select {
	case <-objects.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("outcome upload did not begin")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var row models.WorkflowVisitCheckpoint
	if err := e.db.WithContext(ctx).Take(&row, "id = ?", services.WorkflowCheckpointID(c.run.ID, state.ID, 0)).Error; err != nil {
		close(objects.release)
		t.Fatal("object PUT retained SQL", err)
	}
	if row.RouteState != "sealing" || row.OutcomeRef != "" {
		close(objects.release)
		t.Fatal("dispatch barrier missing before PUT", row)
	}
	if err := e.ResumePausedRun(c.run.ID, 0); !errors.Is(err, services.ErrWorkflowCheckpointBlocked) {
		close(objects.release)
		t.Fatal("generic resume crossed pending checkpoint", err)
	}
	if pool.Stats().InUse != 0 {
		close(objects.release)
		t.Fatal("upload owns SQL connection")
	}
	close(objects.release)
	if err := <-finished; err == nil {
		t.Fatal("failed object upload produced route proof")
	}
	if err := e.db.Select("outcome_ref", "route_state").Take(&row, "id = ?", row.ID).Error; err != nil || row.OutcomeRef != "" {
		t.Fatal("dangling outcome ref", row, err)
	}
}

func TestWorkspaceProviderAttemptDispatchedVisitNeverReplaysOnStartup(t *testing.T) {
	e, c, node, state := workspaceCheckpointFixture(t, true)
	req := e.nodeReq(c, node)
	attempt, err := req.PrepareProviderAttempt(t.Context())
	if err != nil || attempt.Attempt != 1 {
		t.Fatal(attempt, err)
	}
	attempt.Resource, attempt.SessionID, attempt.Workspace, attempt.Authorization = "original-resource", "native-session", "/workspace", "current-grant"
	resource := models.Sandbox{Name: attempt.Resource, Purpose: "run", RunID: req.RunID, NodeID: req.NodeID, StateRunID: state.ID, Iteration: 1, RunAttempt: req.RunAttempt, ProviderAttempt: attempt.Attempt, Status: "running"}
	if err := e.db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if err := req.BeforeProviderDispatch(t.Context(), attempt); err != nil {
		t.Fatal(err)
	}
	if _, err := req.PrepareProviderAttempt(t.Context()); !errors.Is(err, runtime.ErrProviderDispatchUnknown) {
		t.Fatal("dispatched operation repeated", err)
	}
	// This fixture represents a pre-receipt interrupted row. It cannot prove
	// native operation identity and must remain unknown without replay.
	if err := e.db.Table("state_runs").Where("id = ?", state.ID).Update("provider_operation_receipt", "").Error; err != nil {
		t.Fatal(err)
	}
	e.reconcileInterrupted()
	var after models.StateRun
	if err := e.db.First(&after, state.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.WorkspaceCheckpointID == "" {
		t.Fatal("missing durable unknown checkpoint", after)
	}
	row, err := e.workspaceCheckpoints.Get(t.Context(), after.WorkspaceCheckpointID)
	if err != nil || row.RouteState != "unknown" || row.FileState != "unknown" {
		t.Fatal(row, err)
	}
	if err := e.ResumeFrom(c.run.ID, node.ID); !errors.Is(err, services.ErrWorkflowCheckpointBlocked) {
		t.Fatal("manual resume replays unknown operation", err)
	}
	var count int64
	e.db.Model(&models.StateRun{}).Where("run_id = ?", c.run.ID).Count(&count)
	if count != 1 {
		t.Fatal("startup created a new attempt", count)
	}
}

func TestWorkspaceCheckpointSealedBodyCarriesLogicalPayloadNotNestedOwnership(t *testing.T) {
	e, c, node, state := workspaceCheckpointFixture(t, false)
	literal := "fileref:v1/json/" + strings.Repeat("a", 64)
	c.vars["literal"] = literal
	if err := e.db.Create(&models.RunVariable{RunID: c.run.ID, Name: "literal", Value: literal}).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.sealWorkspaceCheckpoint(t.Context(), c, node, nodeOutcome{status: "completed", outputs: map[string]any{"literal": literal}}, finishOpts{}); err != nil {
		t.Fatal(err)
	}
	row, err := e.workspaceCheckpoints.Get(t.Context(), services.WorkflowCheckpointID(c.run.ID, state.ID, 0))
	if err != nil {
		t.Fatal(err)
	}
	body, err := e.workspaceCheckpoints.LoadOutcome(t.Context(), row)
	if err != nil {
		t.Fatal(err)
	}
	var plan workspaceRoutePlan
	if err := json.Unmarshal(body, &plan); err != nil || plan.State.Outputs["literal"] != literal || plan.Variables[0].Value != literal {
		t.Fatal("literal data reinterpreted", err)
	}
}

func TestWorkspaceCheckpointRetainsExactVisitBeforeSharedReuseAndReadyApplies(t *testing.T) {
	e, c, node, state := workspaceCheckpointFixture(t, true)
	ctx := context.Background()
	lineage, err := e.execution.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state.ProviderAttempt = 3
	state.SandboxReadyAt = &now
	if err := e.db.Model(&models.StateRun{}).Where("id = ?", state.ID).Updates(map[string]any{"provider_attempt": 3, "sandbox_ready_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	resource := models.Sandbox{Name: "shared-workflow-resource", Purpose: "run", RunID: c.run.ID, NodeID: node.ID, StateRunID: state.ID, Iteration: 1, RunAttempt: 0, ProviderAttempt: 3, Status: "running"}
	if err := e.db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	var captures atomic.Int32
	fingerprint := strings.Repeat("b", 64)
	checkpoints, err := services.NewWorkflowWorkspaceCheckpoints(e.db, services.WorkflowCheckpointOptions{Enabled: true, Execution: e.execution, AccountExists: func(actor string) bool { return actor == "alice" }, ReadScope: func(context.Context, *gorm.DB, string, string, string) error { return nil }, ResolveBinding: func(context.Context, string, string, string, uint) (services.WorkspaceActiveBinding, error) {
		return services.WorkspaceActiveBinding{SandboxID: resource.ID, Resource: resource.Name, Lineage: lineage, Fingerprint: fingerprint, Reference: sandbox.WorkspaceBindingReference{ID: "original", Revision: 1, Fingerprint: strings.Repeat("c", 64)}}, nil
	}, Capture: func(ctx context.Context, actor string, input services.WorkspaceCaptureInput) (services.WorkspaceSnapshotMetadata, error) {
		captures.Add(1)
		var row models.WorkflowVisitCheckpoint
		if err := e.db.WithContext(ctx).Take(&row, "run_id = ?", c.run.ID).Error; err != nil {
			return services.WorkspaceSnapshotMetadata{}, err
		}
		if row.CaptureStartedAt == nil || row.SnapshotID == "" {
			return services.WorkspaceSnapshotMetadata{}, errors.New("missing durable admission boundary")
		}
		snapshot := models.WorkspaceSnapshot{ID: row.SnapshotID, ActorID: actor, ScopeKind: "run", ScopeID: c.run.ID, RunID: c.run.ID, NodeID: node.ID, StateRunID: state.ID, Iteration: 1, RunAttempt: 0, ProviderAttempt: 3, SourceSandboxID: resource.ID, Lineage: lineage, SourceBindingFingerprint: fingerprint, State: "captured"}
		return services.WorkspaceSnapshotMetadata{}, e.db.WithContext(ctx).Create(&snapshot).Error
	}, Reconcile: func(context.Context, string, string, string, string) (services.WorkspaceSnapshotMetadata, error) {
		return services.WorkspaceSnapshotMetadata{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	e.workspaceCheckpoints = checkpoints
	if err := e.sealWorkspaceCheckpoint(ctx, c, node, nodeOutcome{status: "completed", outputMd: "exact provider 3 result"}, finishOpts{trigger: triggerAuto}); err != nil {
		t.Fatal(err)
	}
	row, err := checkpoints.Get(ctx, services.WorkflowCheckpointID(c.run.ID, state.ID, 0))
	if err != nil {
		t.Fatal(err)
	}
	if row.ProviderAttempt != 3 || row.SandboxID != resource.ID || row.CaptureStartedAt == nil || row.FileState != "pending" {
		t.Fatal("physical provenance lost", row)
	}
	if err := e.executionTransaction(ctx, c.run.ID, func(tx *gorm.DB) error { return services.GuardWorkflowNativeTx(tx.Statement.Context, tx, c.run.ID) }); !errors.Is(err, services.ErrWorkflowCheckpointBlocked) {
		t.Fatal("shared re-prime crossed incomplete capture", err)
	}
	if err := e.advanceWorkspaceCheckpoint(ctx, row); err != nil {
		t.Fatal(err)
	}
	var run models.Run
	e.db.Select("status").First(&run, "id = ?", c.run.ID)
	if run.Status != "paused" {
		t.Fatal("captured but unverified files released disk", run.Status)
	}
	files, _ := filestore.FromDB(e.db)
	archive, err := files.Prepare(ctx, strings.NewReader("verified archive fixture"), filestore.Binary)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := files.Prepare(ctx, strings.NewReader(`{"verified":true}`), filestore.JSON)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.Table("workspace_snapshots").Where("id = ?", row.SnapshotID).Updates(map[string]any{"state": "ready", "verified_at": now, "archive_ref": archive.Ref(), "manifest_ref": manifest.Ref()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.advanceWorkspaceCheckpoint(ctx, row); err != nil {
		t.Fatal(err)
	}
	if captures.Load() != 1 {
		t.Fatal("capture re-admitted", captures.Load())
	}
	if err := e.executionTransaction(ctx, c.run.ID, func(tx *gorm.DB) error { return services.GuardWorkflowNativeTx(tx.Statement.Context, tx, c.run.ID) }); err != nil {
		t.Fatal("ready/applied checkpoint did not release shared workspace", err)
	}
	if err := e.db.Select("status").First(&run, "id = ?", c.run.ID).Error; err != nil || run.Status != "queued" {
		t.Fatal(run.Status, err)
	}
}

func TestWorkspaceCheckpointFailureAndCancelNeverInventHistoricalFiles(t *testing.T) {
	for _, mode := range []string{"missing-source", "missing-actor", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			e, c, node, state := workspaceCheckpointFixture(t, true)
			if mode == "cancel" {
				if err := e.Cancel(c.run.ID); err != nil {
					t.Fatal(err)
				}
				var got models.StateRun
				e.db.First(&got, state.ID)
				row, err := e.workspaceCheckpoints.Get(t.Context(), got.WorkspaceCheckpointID)
				if err != nil || row.FileState != "unknown" || row.RouteState != "unknown" {
					t.Fatal(row, err)
				}
				return
			}
			now := time.Now().UTC()
			e.db.Model(&models.StateRun{}).Where("id = ?", state.ID).Update("sandbox_ready_at", now)
			if mode == "missing-actor" {
				c.run.WorkspaceActorID = ""
				e.db.Table("runs").Where("id = ?", c.run.ID).Update("workspace_actor_id", "")
				e.db.Model(&models.StateRun{}).Where("id = ?", state.ID).Update("provider_attempt", 1)
				e.db.Create(&models.Sandbox{Name: "actor-unavailable", Purpose: "run", RunID: c.run.ID, NodeID: node.ID, StateRunID: state.ID, Iteration: 1, ProviderAttempt: 1, Status: "running"})
			}
			if err := e.sealWorkspaceCheckpoint(t.Context(), c, node, nodeOutcome{status: "failed", err: "known agent failure"}, finishOpts{trigger: triggerAuto}); err != nil {
				t.Fatal(err)
			}
			row, err := e.workspaceCheckpoints.Get(t.Context(), services.WorkflowCheckpointID(c.run.ID, state.ID, 0))
			if err != nil {
				t.Fatal(err)
			}
			if row.FileState != "unavailable" && row.FileState != "blocked" {
				t.Fatal("current disk mislabelled ready", row)
			}
			if err := e.advanceWorkspaceCheckpoint(t.Context(), row); !errors.Is(err, services.ErrWorkflowCheckpointBlocked) {
				t.Fatal("blocked provenance continued", err)
			}
		})
	}
}
