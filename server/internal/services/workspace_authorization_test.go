package services

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadhold"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

func workspaceAuthorizationFixture(t *testing.T) (*WorkspaceAuthorization, models.WorkspaceSnapshotJob, map[string]string, *atomic.Bool) {
	t.Helper()
	sbx, agent, _, credentials := authorizationFixture(t)
	db := sbx.db
	if err := db.AutoMigrate(&models.WorkspaceSnapshot{}, &models.WorkspaceSnapshotJob{}); err != nil {
		t.Fatal(err)
	}
	execution, _, _ := serviceExecutionController(t, db)
	if err := sbx.skills.Save(agent); err != nil {
		t.Fatal(err)
	}
	agent = sbx.effectiveAgent(agent)
	grant, err := sbx.currentAuthorizationFingerprint(t.Context(), agent)
	if err != nil {
		t.Fatal(err)
	}
	resource := models.Sandbox{Name: "authorized-workspace-resource", Profile: agent.Name, Purpose: SandboxPurposeSolo, ThreadID: "authorized-solo", Status: "running", AuthorizationFingerprint: grant}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	solo := models.SoloSession{ID: resource.ThreadID, UserID: "alice", AgentName: agent.Name, SandboxID: resource.ID, Status: models.SoloStatusRunning, InitialConfigurationID: agent.ModelBinding.ConfigurationID, AppliedConfigurationID: agent.ModelBinding.ConfigurationID, CurrentConfigurationID: agent.ModelBinding.ConfigurationID, LifecycleEpoch: 3, LifecycleAttemptID: "current-physical-attempt"}
	if err := db.Create(&solo).Error; err != nil {
		t.Fatal(err)
	}
	active := &atomic.Bool{}
	active.Store(true)
	resolve := func(ctx context.Context, actor, kind, scope string, id uint) (WorkspaceRestoreBinding, error) {
		if actor != "alice" || kind != "solo" || scope != solo.ID || id != resource.ID {
			return WorkspaceRestoreBinding{}, gorm.ErrRecordNotFound
		}
		return WorkspaceRestoreBinding{SandboxID: id, Fingerprint: strings.Repeat("b", 64)}, nil
	}
	authorization, err := NewWorkspaceAuthorization(db, sbx, execution, WorkspaceAuthorizationOptions{AccountExists: func(actor string) bool { return active.Load() && (actor == "alice" || actor == "bob") }, ResolveBinding: resolve, CurrentBinding: func(ctx context.Context, tx *gorm.DB, actor, kind, scope string, id uint) (WorkspaceRestoreBinding, error) {
		if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside { // Metadata preflight may use a plain connection; no provider is called.
			return resolve(ctx, actor, kind, scope, id)
		}
		return WorkspaceRestoreBinding{SandboxID: id, Fingerprint: strings.Repeat("b", 64)}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	job := models.WorkspaceSnapshotJob{ID: "new-authenticated-job", ActorID: "alice", ScopeKind: "solo", ScopeID: solo.ID, SandboxID: resource.ID, Kind: "capture_files", Deadline: time.Now().UTC().Add(time.Minute)}
	return authorization, job, credentials, active
}

func TestWorkspaceAuthorizationRequiresCurrentAccountOwnerAndIndependentSharedRunPolicy(t *testing.T) {
	a, job, _, active := workspaceAuthorizationFixture(t)
	ctx, err := a.PrepareJob(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return a.AuthorizeJob(ctx, tx, job) }); err != nil {
		t.Fatal(err)
	}
	if err := a.ReadScope(t.Context(), a.db, "bob", "solo", job.ScopeID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("foreign Solo was disclosed", err)
	}
	if err := a.db.Table("runs").Create(map[string]any{"id": "shared-run", "created_by": "bob", "status": "completed"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.ReadScope(t.Context(), a.db, "alice", "run", "shared-run"); err != nil {
		t.Fatal("CreatedBy was mistaken for workspace ownership", err)
	}
	if err := a.db.Table("runs").Create(map[string]any{"id": "solo-derived-run", "created_by": "bob", "origin_solo_id": job.ScopeID, "status": "completed"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.ReadScope(t.Context(), a.db, "bob", "run", "solo-derived-run"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("shared read bypassed an OriginSolo owner", err)
	}
	active.Store(false)
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return a.AuthorizeJob(ctx, tx, job) }); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("removed account retained a prepared grant", err)
	}
}

func TestWorkspaceAuthorizationDetectsAllCredentialChangesInsideMetadataOnlyFence(t *testing.T) {
	for _, kind := range []string{"model", "git", "extra", "mcp"} {
		t.Run(kind, func(t *testing.T) {
			a, job, credentials, _ := workspaceAuthorizationFixture(t)
			ctx, err := a.PrepareJob(t.Context(), job)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.db.Model(&models.Credential{}).Where("id = ?", credentials[kind]).Update("secret_revision", gorm.Expr("secret_revision + 1")).Error; err != nil {
				t.Fatal(err)
			}
			if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return a.AuthorizeJob(ctx, tx, job) }); !errors.Is(err, ErrWorkspaceJobConflict) {
				t.Fatal("same-value rotation crossed prepared publication", err)
			}
		})
	}
}

func TestWorkspaceAuthorizationRejectsUnpreparedExpiredOrDifferentJobProof(t *testing.T) {
	a, job, _, _ := workspaceAuthorizationFixture(t)
	if err := a.db.Transaction(func(tx *gorm.DB) error { return a.AuthorizeJob(t.Context(), tx, job) }); !errors.Is(err, ErrWorkspaceJobAuthority) {
		t.Fatal("caller invented its execution proof", err)
	}
	ctx, err := a.PrepareJob(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	other := job
	other.Body = `{"changed":true}`
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return a.AuthorizeJob(ctx, tx, other) }); !errors.Is(err, ErrWorkspaceJobAuthority) {
		t.Fatal("proof authorized another immutable job", err)
	}
	proof := ctx.Value(workspaceAuthorizationKey{}).(workspaceAuthorizationProof)
	proof.expires = time.Now().Add(-time.Second)
	expired := context.WithValue(ctx, workspaceAuthorizationKey{}, proof)
	if err := a.db.WithContext(expired).Transaction(func(tx *gorm.DB) error { return a.AuthorizeJob(expired, tx, job) }); !errors.Is(err, ErrWorkspaceJobAuthority) {
		t.Fatal("expired proof refreshed itself inside SQL", err)
	}
	if err := a.AuthorizeJob(expired, a.db, job); !errors.Is(err, ErrWorkspaceJobAuthority) {
		t.Fatal("expired execution preflight fell back to a historical receipt read", err)
	}
}

func TestWorkspaceAuthorizationPreparedCommitUsesOnlyCurrentSQLMetadata(t *testing.T) {
	a, job, _, _ := workspaceAuthorizationFixture(t)
	ctx, err := a.PrepareJob(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	// Holding the only connection would deadlock an accidental transaction
	// callback which resolves P1 secrets through a second DB connection.
	pool, err := a.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	a.sandbox.modelBinding = func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
		t.Fatal("fenced commit resolved secrets or current models inside SQL")
		return modelrouting.Resolved{}, ErrWorkspaceJobAuthority
	}
	a.options.ResolveBinding = func(context.Context, string, string, string, uint) (WorkspaceRestoreBinding, error) {
		t.Fatal("fenced commit made a native binding GET inside SQL")
		return WorkspaceRestoreBinding{}, ErrWorkspaceJobAuthority
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := a.db.WithContext(bounded).Transaction(func(tx *gorm.DB) error { return a.AuthorizeJob(bounded, tx, job) }); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Model(&models.SoloSession{}).Where("id = ?", job.ScopeID).Update("lifecycle_epoch", gorm.Expr("lifecycle_epoch + 1")).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.WithContext(bounded).Transaction(func(tx *gorm.DB) error { return a.AuthorizeJob(bounded, tx, job) }); !errors.Is(err, ErrWorkspaceJobConflict) {
		t.Fatal("a changed parent accepted the old prepared proof", err)
	}
}

func TestWorkspaceHoldProofBindsRealPublicationAndCanonicalDeadline(t *testing.T) {
	a, job, _, _ := workspaceAuthorizationFixture(t)
	job.ID, job.Kind, job.Lineage = "publication-authorization-job", "read_snapshot", "original-lineage"
	job.Deadline = time.Date(2026, 11, 9, 12, 0, 0, 123456789, time.UTC)
	job.Body = `{"sourceJobId":"captured-auth-source"}`
	if err := a.db.AutoMigrate(&models.WorkspaceSnapshotJob{}, &models.WorkspaceSnapshot{}); err != nil {
		t.Fatal(err)
	}
	if err := a.db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.Create(&models.WorkspaceSnapshot{ID: "captured-auth-source", ActorID: "alice", ScopeKind: "solo", ScopeID: job.ScopeID, JobID: "captured-auth-source", Lineage: job.Lineage}).Error; err != nil {
		t.Fatal(err)
	}
	id := payloadhold.Identity{OperationID: job.ID, SnapshotID: "captured-auth-source", ActorID: job.ActorID, ScopeKind: job.ScopeKind, ScopeID: job.ScopeID, Lineage: job.Lineage, Deadline: job.Deadline.Truncate(time.Millisecond)}
	ctx, err := a.PrepareHold(t.Context(), id)
	if err != nil {
		t.Fatal("P6 canonical millisecond deadline lost real job ownership", err)
	}
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return a.AuthorizeHold(ctx, tx, id) }); err != nil {
		t.Fatal(err)
	}
	id.SnapshotID = "different-claimed-source"
	if _, err := a.PrepareHold(t.Context(), id); !errors.Is(err, ErrWorkspaceJobConflict) {
		t.Fatal("unrelated hold referenced the real publication operation", err)
	}
}

func TestWorkspaceRestoreSourceReadIsIndependentOfOriginalActorAndExecutionLineage(t *testing.T) {
	f := restoreFixture(t)
	db := f.service.db
	closed, _ := executionlineage.NewLocalController(t.Context(), nil)
	unused := func(context.Context, string, string, string, uint) (WorkspaceRestoreBinding, error) {
		t.Fatal("historical source read resolved a native execution binding")
		return WorkspaceRestoreBinding{}, ErrWorkspaceJobAuthority
	}
	a, err := NewWorkspaceAuthorization(db, f.service.Jobs.sandbox, closed, WorkspaceAuthorizationOptions{AccountExists: func(actor string) bool { return actor == "alice" || actor == "bob" }, ResolveBinding: unused, CurrentBinding: func(context.Context, *gorm.DB, string, string, string, uint) (WorkspaceRestoreBinding, error) {
		t.Fatal("historical source read used execution binding")
		return WorkspaceRestoreBinding{}, ErrWorkspaceJobAuthority
	}})
	if err != nil {
		t.Fatal(err)
	}
	f.service.options.AuthorizeSnapshot = a.ReadSnapshot
	if err := db.Table("runs").Create(map[string]any{"id": "shared-historical-source", "created_by": "bob", "status": "completed"}).Error; err != nil {
		t.Fatal(err)
	}
	var source models.WorkspaceSnapshot
	if err := db.Take(&source, "id = ?", f.input.SnapshotID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkspaceSnapshot{}).Where("id = ?", source.ID).Updates(map[string]any{"actor_id": "bob", "scope_kind": "run", "scope_id": "shared-historical-source", "run_id": "shared-historical-source", "lineage": "previous-restored-dataset"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.WorkspaceSnapshotJob{}).Where("id IN ?", []string{source.JobID, source.PublicationJobID}).Updates(map[string]any{"actor_id": "bob", "scope_kind": "run", "scope_id": "shared-historical-source", "lineage": "previous-restored-dataset"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := f.service.source(t.Context(), db, "alice", source.ID); err != nil {
		t.Fatal("new target actor could not read an authorized shared historical source", err)
	}
	// Provenance remains bound to the original capture actor, even though
	// current read permission admits a different authenticated account.
	if err := db.Model(&models.WorkspaceSnapshotJob{}).Where("id = ?", source.PublicationJobID).Update("actor_id", "alice").Error; err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := f.service.source(t.Context(), db, "alice", source.ID); !errors.Is(err, ErrWorkspaceJobConflict) {
		t.Fatal("source actor provenance followed the new target actor", err)
	}
	if err := db.Model(&models.WorkspaceSnapshotJob{}).Where("id = ?", source.PublicationJobID).Update("actor_id", "bob").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{ID: "private-source-parent", UserID: "bob"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("runs").Where("id = ?", "shared-historical-source").Update("origin_solo_id", "private-source-parent").Error; err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := f.service.source(t.Context(), db, "alice", source.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("withdrawn source read scope was disclosed", err)
	}
	if _, _, _, _, err := f.service.source(t.Context(), db, "alice", "missing-source-id"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("missing and unauthorized source status differ", err)
	}
}

type workspaceReadOnlyReconcileRemote struct{ gets, creates atomic.Int64 }

func (r *workspaceReadOnlyReconcileRemote) Capabilities(context.Context) (sandbox.WorkspaceJobCapabilities, error) {
	return sandbox.WorkspaceJobCapabilities{}, ErrWorkspaceJobUnsupported
}
func (r *workspaceReadOnlyReconcileRemote) Create(context.Context, sandbox.WorkspaceJobCreate) (sandbox.WorkspaceJobReceipt, error) {
	r.creates.Add(1)
	return sandbox.WorkspaceJobReceipt{}, ErrWorkspaceJobUnknown
}
func (r *workspaceReadOnlyReconcileRemote) Get(context.Context, string) (sandbox.WorkspaceJobReceipt, error) {
	r.gets.Add(1)
	return sandbox.WorkspaceJobReceipt{}, ErrWorkspaceJobUnknown
}
func (r *workspaceReadOnlyReconcileRemote) Renew(context.Context, string, sandbox.WorkspaceJobRenew) (sandbox.WorkspaceJobReceipt, error) {
	return sandbox.WorkspaceJobReceipt{}, ErrWorkspaceJobOwner
}
func (r *workspaceReadOnlyReconcileRemote) Cancel(context.Context, string, sandbox.WorkspaceJobRenew) (sandbox.WorkspaceJobReceipt, error) {
	return sandbox.WorkspaceJobReceipt{}, ErrWorkspaceJobUnknown
}

func TestWorkspaceStrictReceiptReadDoesNotMintExecutionButReconcilePreparesRealJob(t *testing.T) {
	a, job, _, _ := workspaceAuthorizationFixture(t)
	a.sandbox.execution = a.execution
	lineage, err := a.execution.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	job.Lineage, job.State, job.DispatchedAt = lineage, "unknown", &now
	job.RuntimeOwner, job.RuntimeResource, job.RuntimeGeneration, job.RuntimeSHA256 = "original-remote-owner", "authorized-workspace-resource", 1, strings.Repeat("a", 64)
	if err := a.db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	var prepared atomic.Int64
	resolve := a.options.ResolveBinding
	a.options.ResolveBinding = func(ctx context.Context, actor, kind, scope string, id uint) (WorkspaceRestoreBinding, error) {
		prepared.Add(1)
		return resolve(ctx, actor, kind, scope, id)
	}
	remote := &workspaceReadOnlyReconcileRemote{}
	options, err := a.JobOptions(WorkspaceJobOptions{Remote: func(context.Context, uint) (WorkspaceJobRemote, error) { return remote, nil }})
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewWorkspaceSnapshotJobs(a.db, a.sandbox, options)
	if _, err := jobs.Get(t.Context(), job.ActorID, job.ID); err != nil {
		t.Fatal("pure receipt read required an execution proof", err)
	}
	if prepared.Load() != 0 || remote.gets.Load() != 0 {
		t.Fatal("receipt read resolved an execution binding or contacted the provider")
	}
	if _, err := jobs.Reconcile(t.Context(), job.ActorID, job.ID); !errors.Is(err, ErrWorkspaceJobUnknown) {
		t.Fatal("reconciliation did not preserve unknown dispatch", err)
	}
	if prepared.Load() == 0 || remote.gets.Load() != 1 || remote.creates.Load() != 0 {
		t.Fatal("explicit reconciliation skipped the typed preparation or replayed its native job", prepared.Load(), remote.gets.Load(), remote.creates.Load())
	}
}

func TestWorkspaceCaptureAuthorizationKeepsOriginalLogicalBindingAtPublication(t *testing.T) {
	a, job, _, _ := workspaceAuthorizationFixture(t)
	source := models.WorkspaceSnapshot{ID: job.ID, JobID: job.ID, ActorID: job.ActorID, ScopeKind: job.ScopeKind, ScopeID: job.ScopeID, SourceBindingFingerprint: strings.Repeat("b", 64)}
	if err := a.db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	ctx, err := a.PrepareJob(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.Model(&models.WorkspaceSnapshot{}).Where("id = ?", source.ID).Update("source_binding_fingerprint", strings.Repeat("c", 64)).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return a.AuthorizeJob(ctx, tx, job) }); !errors.Is(err, ErrWorkspaceJobConflict) {
		t.Fatal("publication accepted a different frozen source binding", err)
	}
	if _, err := a.PrepareJob(t.Context(), job); !errors.Is(err, ErrWorkspaceJobConflict) {
		t.Fatal("a new current grant authorized data captured under a different logical binding", err)
	}
}

func TestWorkspaceConfirmedReleaseBlocksPreparedJobsAndNewPhysicalLeasesButKeepsReceipts(t *testing.T) {
	a, job, _, _ := workspaceAuthorizationFixture(t)
	if err := a.db.AutoMigrate(&models.WorkspaceRelease{}); err != nil {
		t.Fatal(err)
	}
	a.sandbox.workspaceReleaseGuard = GuardWorkspaceReleaseTx
	ctx, err := a.PrepareJob(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.db.Create(&models.WorkspaceRelease{ID: "confirmed-whole-release", SandboxID: job.SandboxID, SoloID: job.ScopeID, ActorID: job.ActorID, State: "confirmed"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return a.AuthorizeJob(ctx, tx, job) }); !errors.Is(err, ErrWorkspaceReleaseBlocked) {
		t.Fatal("old prepared job crossed a confirmed release", err)
	}
	a.sandbox.modelBinding = func(string, *modelrouting.Binding) (modelrouting.Resolved, error) {
		t.Fatal("blocked new job resolved provider credentials")
		return modelrouting.Resolved{}, ErrWorkspaceJobAuthority
	}
	if _, err := a.PrepareJob(t.Context(), job); !errors.Is(err, ErrWorkspaceReleaseBlocked) {
		t.Fatal("confirmed release admitted a new file job", err)
	}
	if _, release, err := a.sandbox.acquireSandboxOperation(t.Context(), job.SandboxID, "workspace_capture_files"); !errors.Is(err, ErrWorkspaceReleaseBlocked) {
		if release != nil {
			release()
		}
		t.Fatal("confirmed release admitted a new physical lease", err)
	}
	if err := a.db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	options, err := a.JobOptions(WorkspaceJobOptions{})
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewWorkspaceSnapshotJobs(a.db, a.sandbox, options)
	if _, err := jobs.Get(t.Context(), job.ActorID, job.ID); err != nil {
		t.Fatal("environment release hid an accepted receipt", err)
	}
}

func TestWorkspaceAuthorizationExpiryWhileWaitingForFinalMetadataFencePreventsCommit(t *testing.T) {
	a, job, _, _ := workspaceAuthorizationFixture(t)
	ctx, err := a.PrepareJob(t.Context(), job)
	if err != nil {
		t.Fatal(err)
	}
	proof := ctx.Value(workspaceAuthorizationKey{}).(workspaceAuthorizationProof)
	proof.expires = time.Now().Add(250 * time.Millisecond)
	ctx = context.WithValue(ctx, workspaceAuthorizationKey{}, proof)
	var entered atomic.Bool
	a.sandbox.workspaceReleaseGuard = func(context.Context, *gorm.DB, uint) error {
		entered.Store(true)
		// A contended release row may be obtained after the original private
		// proof expires. This final fence cannot renew that proof in SQL.
		timer := time.NewTimer(time.Until(proof.expires) + time.Millisecond)
		defer timer.Stop()
		<-timer.C
		return nil
	}
	if err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return a.AuthorizeJob(ctx, tx, job) }); !errors.Is(err, ErrWorkspaceJobAuthority) || !entered.Load() {
		t.Fatal("late metadata fence extended publication permission", err, entered.Load())
	}
}

func TestWorkspaceAuthorizationHistoryAndSourceSnapshotReadDoNotRequireOldExecutionGrant(t *testing.T) {
	a, job, _, _ := workspaceAuthorizationFixture(t)
	if err := a.db.Table("runs").Create(map[string]any{"id": "old-shared-source", "created_by": "bob", "status": "completed"}).Error; err != nil {
		t.Fatal(err)
	}
	source := models.WorkspaceSnapshot{ID: "old-ready-source", ActorID: "bob", ScopeKind: "run", ScopeID: "old-shared-source", State: "ready", Lineage: "old-restored-dataset"}
	a.options.ResolveBinding = func(context.Context, string, string, string, uint) (WorkspaceRestoreBinding, error) {
		t.Fatal("history attempted native binding resolution")
		return WorkspaceRestoreBinding{}, nil
	}
	if err := a.AuthorizeSnapshot(t.Context(), a.db, "alice", source); err != nil {
		t.Fatal("old source execution identity hid shared ready snapshot", err)
	}
	if err := a.AuthorizeJob(t.Context(), a.db, job); err != nil {
		t.Fatal("ordinary authorized receipt read required native current grant", err)
	}
}
