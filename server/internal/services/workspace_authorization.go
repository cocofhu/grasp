package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/executionlineage"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/payloadhold"
	"github.com/cocofhu/grasp/internal/workspacefiles"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WorkspaceAuthorizationOptions uses the same live static-account list as
// cookie authentication. Binding resolution must read the native canonical
// head outside SQL; CurrentBinding is a metadata-only, current locking read.
// Neither callback is inferred from caller-supplied hashes or resource names.
type WorkspaceAuthorizationOptions struct {
	AccountExists  func(string) bool
	ResolveBinding func(context.Context, string, string, string, uint) (WorkspaceRestoreBinding, error)
	CurrentBinding func(context.Context, *gorm.DB, string, string, string, uint) (WorkspaceRestoreBinding, error)
}

type WorkspaceAuthorization struct {
	db        *gorm.DB
	sandbox   *SandboxService
	execution *executionlineage.Controller
	options   WorkspaceAuthorizationOptions
}

func NewWorkspaceAuthorization(db *gorm.DB, sbx *SandboxService, execution *executionlineage.Controller, options WorkspaceAuthorizationOptions) (*WorkspaceAuthorization, error) {
	if db == nil || sbx == nil || execution == nil || options.AccountExists == nil || options.ResolveBinding == nil || options.CurrentBinding == nil || messageOutsideTransaction(db) != nil {
		return nil, ErrWorkspaceJobAuthority
	}
	return &WorkspaceAuthorization{db: db, sandbox: sbx, execution: execution, options: options}, nil
}

type workspaceAuthorizationKey struct{}
type workspaceAuthorizationProof struct {
	service  *WorkspaceAuthorization
	expires  time.Time
	job      string
	resource workspaceAuthorizationResource
	scopes   []workspaceAuthorizationScope
	sources  []workspaceAuthorizationSource
	grant    workspaceAuthorizationGrant
	binding  WorkspaceRestoreBinding
}

type workspaceAuthorizationResource struct {
	ID                                                                    uint
	Name, Profile, Purpose, Status, DesiredState, ThreadID, RunID, NodeID string
	StateRunID                                                            uint
	Iteration, RunAttempt, ProviderAttempt                                int
	Generation                                                            uint64
	AuthorizationFingerprint                                              string
}

type workspaceAuthorizationScope struct {
	Kind, ID, Owner, OriginSolo                                      string
	SandboxID                                                        uint
	Epoch                                                            uint64
	LifecycleAttempt, Action, Status, Agent                          string
	InitialConfiguration, AppliedConfiguration, CurrentConfiguration string
	RunAttempt                                                       int
	APIKeyDigest                                                     string
}

type workspaceAuthorizationSource struct {
	ID, ScopeKind, ScopeID, ActorID, JobID, Lineage string
	SourceBindingFingerprint                        string
}

func authorizationDigest(value any) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func authorizationJobIdentity(row models.WorkspaceSnapshotJob) string {
	return authorizationDigest(struct {
		ID, Actor, ScopeKind, ScopeID, Kind, Body, Nonce, Lineage string
		SandboxID                                                 uint
		Deadline                                                  time.Time
	}{row.ID, row.ActorID, row.ScopeKind, row.ScopeID, row.Kind, row.Body, row.Nonce, row.Lineage, row.SandboxID, row.Deadline.UTC()})
}

func workspaceCurrentRead(db *gorm.DB) *gorm.DB {
	db = db.Session(&gorm.Session{NewDB: true})
	if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return db.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return db
}

func (a *WorkspaceAuthorization) account(actor string) error {
	if !validWorkspaceKey(actor) || !strings.HasPrefix(actor, "api-key:") && !a.options.AccountExists(actor) {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ReadScope is ordinary metadata authorization. Independent workflow Runs
// belong to the shared authenticated workspace; CreatedBy is attribution,
// never an ownership rule. Solo-derived Runs retain their Solo ownership.
func (a *WorkspaceAuthorization) ReadScope(ctx context.Context, db *gorm.DB, actor, kind, id string) error {
	_, err := a.readScope(ctx, db, actor, kind, id)
	return err
}

func (a *WorkspaceAuthorization) readScope(ctx context.Context, db *gorm.DB, actor, kind, id string) ([]workspaceAuthorizationScope, error) {
	if err := a.account(actor); err != nil {
		return nil, err
	}
	if db == nil || !validWorkspaceKey(id) {
		return nil, gorm.ErrRecordNotFound
	}
	query := workspaceCurrentRead(db.WithContext(ctx))
	var scopes []workspaceAuthorizationScope
	if kind == "run" {
		var row struct {
			ID, OriginSoloID, Status, WorkflowID, WorkspaceActorID string
			Attempt                                                int
		}
		if err := query.Table("runs").Select("id", "origin_solo_id", "status", "attempt", "workflow_id", "workspace_actor_id").Take(&row, "id = ?", id).Error; err != nil {
			return nil, err
		}
		scope := workspaceAuthorizationScope{Kind: "run", ID: row.ID, OriginSolo: row.OriginSoloID, RunAttempt: row.Attempt}
		if strings.HasPrefix(actor, "api-key:") {
			if row.OriginSoloID != "" || row.WorkspaceActorID != actor {
				return nil, gorm.ErrRecordNotFound
			}
			var key struct{ ID, WorkflowID, KeyHash string }
			if err := workspaceCurrentRead(db.WithContext(ctx)).Table("workflow_api_keys").Select("id", "workflow_id", "key_hash").Where("id = ? AND workflow_id = ? AND revoked_at IS NULL", strings.TrimPrefix(actor, "api-key:"), row.WorkflowID).Take(&key).Error; err != nil {
				return nil, err
			}
			scope.APIKeyDigest = authorizationDigest(key)
		}
		scopes = append(scopes, scope)
		if row.OriginSoloID == "" {
			return scopes, nil
		}
		id, kind = row.OriginSoloID, "solo"
	}
	if kind != "solo" {
		return nil, gorm.ErrRecordNotFound
	}
	if strings.HasPrefix(actor, "api-key:") {
		return nil, gorm.ErrRecordNotFound
	}
	var row struct {
		ID, UserID, AgentName, Status, LifecycleAttemptID, LifecycleAction     string
		SandboxID                                                              uint
		LifecycleEpoch                                                         uint64
		InitialConfigurationID, AppliedConfigurationID, CurrentConfigurationID string
	}
	if err := query.Table("solo_sessions").Select("id", "user_id", "agent_name", "sandbox_id", "status", "lifecycle_epoch", "lifecycle_attempt_id", "lifecycle_action", "initial_configuration_id", "applied_configuration_id", "current_configuration_id").Take(&row, "id = ? AND user_id = ?", id, actor).Error; err != nil {
		return nil, err
	}
	scopes = append(scopes, workspaceAuthorizationScope{Kind: "solo", ID: row.ID, Owner: row.UserID, Agent: row.AgentName, SandboxID: row.SandboxID, Status: row.Status, Epoch: row.LifecycleEpoch, LifecycleAttempt: row.LifecycleAttemptID, Action: row.LifecycleAction, InitialConfiguration: row.InitialConfigurationID, AppliedConfiguration: row.AppliedConfigurationID, CurrentConfiguration: row.CurrentConfigurationID})
	return scopes, nil
}

func workspaceResource(db *gorm.DB, id uint) (workspaceAuthorizationResource, error) {
	var resource workspaceAuthorizationResource
	err := workspaceCurrentRead(db).Table("sandboxes").Select("id", "name", "profile", "purpose", "status", "desired_state", "thread_id", "run_id", "node_id", "state_run_id", "iteration", "run_attempt", "provider_attempt", "generation", "authorization_fingerprint").Take(&resource, id).Error
	return resource, err
}

func (a *WorkspaceAuthorization) sources(ctx context.Context, db *gorm.DB, row models.WorkspaceSnapshotJob) ([]workspaceAuthorizationSource, error) {
	id := row.ID
	switch row.Kind {
	case "activate_workspace", "return_workspace":
		snapshots, err := WorkspaceBindingJobSources(ctx, db, row)
		if err != nil {
			return nil, err
		}
		sources := make([]workspaceAuthorizationSource, 0, len(snapshots))
		for _, snapshot := range snapshots {
			sources = append(sources, workspaceAuthorizationSource{ID: snapshot.ID, ScopeKind: snapshot.ScopeKind, ScopeID: snapshot.ScopeID, ActorID: snapshot.ActorID, JobID: snapshot.JobID, Lineage: snapshot.Lineage, SourceBindingFingerprint: snapshot.SourceBindingFingerprint})
		}
		return sources, nil
	case "capture_files":
	case "read_snapshot":
		var spec workspacefiles.ReadSpec
		if workspacefiles.Decode([]byte(row.Body), &spec) != nil || spec.SourceJobID == "" {
			return nil, ErrWorkspaceJobInvalid
		}
		id = spec.SourceJobID
	case "restore_files":
		var restore models.WorkspaceRestore
		err := db.WithContext(ctx).Select("id", "snapshot_id").Take(&restore, "id = ?", row.ID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if prior, ok := ctx.Value(workspaceRestoreAdmissionKey{}).(workspaceRestoreAdmission); ok && prior.row.ID == row.ID && prior.row.ActorID == row.ActorID {
				restore = prior.row
			} else {
				return nil, ErrWorkspaceJobInvalid
			}
		} else if err != nil {
			return nil, err
		}
		id = restore.SnapshotID
	default:
		return nil, ErrWorkspaceJobUnsupported
	}
	var source workspaceAuthorizationSource
	if err := workspaceCurrentRead(db.WithContext(ctx)).Table("workspace_snapshots").Select("id", "scope_kind", "scope_id", "actor_id", "job_id", "lineage", "source_binding_fingerprint").Take(&source, "id = ?", id).Error; err != nil {
		if row.Kind == "capture_files" && errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return []workspaceAuthorizationSource{source}, nil
}

// PrepareJob resolves secrets and canonical binding only before the outermost
// transaction. The resulting proof is private, short-lived and bound to the
// complete immutable job input; it never upgrades a receipt read to execution.
func (a *WorkspaceAuthorization) PrepareJob(ctx context.Context, row models.WorkspaceSnapshotJob) (context.Context, error) {
	if err := messageOutsideTransaction(a.db); err != nil {
		return ctx, err
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	expires, _ := check.Deadline()
	scopes, err := a.readScope(check, a.db, row.ActorID, row.ScopeKind, row.ScopeID)
	if err != nil {
		return ctx, err
	}
	sources, err := a.sources(check, a.db, row)
	if err != nil {
		return ctx, err
	}
	for _, source := range sources {
		more, err := a.readScope(check, a.db, row.ActorID, source.ScopeKind, source.ScopeID)
		if err != nil {
			return ctx, err
		}
		scopes = append(scopes, more...)
	}
	if err := a.sandbox.checkWorkspaceRelease(check, row.SandboxID); err != nil {
		return ctx, err
	}
	resource, err := workspaceResource(a.db.WithContext(check), row.SandboxID)
	if err != nil {
		return ctx, err
	}
	if resource.Name == "" || resource.Profile == "" || resource.Status != "running" || (resource.DesiredState != "" && resource.DesiredState != "running") {
		return ctx, ErrWorkspaceJobUnsupported
	}
	if row.ScopeKind == "solo" && (resource.Purpose != SandboxPurposeSolo || resource.ThreadID != row.ScopeID) {
		return ctx, gorm.ErrRecordNotFound
	}
	if row.ScopeKind == "run" && (resource.Purpose != "run" || resource.RunID != row.ScopeID || resource.StateRunID == 0 || resource.NodeID == "" || resource.Iteration < 1) {
		return ctx, ErrWorkspaceJobUnsupported
	}
	grant, err := a.prepareGrant(check, resource)
	if err != nil {
		return ctx, err
	}
	binding, err := a.options.ResolveBinding(check, row.ActorID, row.ScopeKind, row.ScopeID, row.SandboxID)
	if err != nil {
		return ctx, err
	}
	if binding.SandboxID != row.SandboxID || binding.Fingerprint == "" {
		return ctx, ErrWorkspaceJobUnsupported
	}
	if row.Kind != "restore_files" && row.Kind != "activate_workspace" && row.Kind != "return_workspace" {
		for _, source := range sources {
			if source.SourceBindingFingerprint != "" && source.SourceBindingFingerprint != binding.Fingerprint {
				return ctx, ErrWorkspaceJobConflict
			}
		}
	}
	after, err := a.prepareGrant(check, resource)
	if err != nil {
		return ctx, err
	}
	if authorizationDigest(grant) != authorizationDigest(after) || !expires.After(time.Now()) {
		return ctx, ErrWorkspaceJobConflict
	}
	slices.SortFunc(scopes, func(x, y workspaceAuthorizationScope) int {
		return strings.Compare(x.Kind+"\x00"+x.ID, y.Kind+"\x00"+y.ID)
	})
	scopes = slices.CompactFunc(scopes, func(x, y workspaceAuthorizationScope) bool { return x.Kind == y.Kind && x.ID == y.ID })
	proof := workspaceAuthorizationProof{service: a, expires: expires, job: authorizationJobIdentity(row), resource: resource, scopes: scopes, sources: sources, grant: grant, binding: binding}
	return context.WithValue(ctx, workspaceAuthorizationKey{}, proof), nil
}

func (a *WorkspaceAuthorization) proof(ctx context.Context, row models.WorkspaceSnapshotJob) (workspaceAuthorizationProof, error) {
	proof, ok := ctx.Value(workspaceAuthorizationKey{}).(workspaceAuthorizationProof)
	if !ok || proof.service != a || proof.job != authorizationJobIdentity(row) || !proof.expires.After(time.Now()) {
		return workspaceAuthorizationProof{}, ErrWorkspaceJobAuthority
	}
	return proof, nil
}

func (a *WorkspaceAuthorization) AuthorizeJob(ctx context.Context, db *gorm.DB, row models.WorkspaceSnapshotJob) error {
	if err := a.account(row.ActorID); err != nil {
		return err
	}
	proof, err := a.proof(ctx, row)
	_, inside := db.Statement.ConnPool.(gorm.TxCommitter)
	if err != nil && !inside && ctx.Value(workspaceAuthorizationKey{}) == nil {
		// Historical generic Get has no execution proof. Product history uses
		// a dedicated metadata DTO and never calls the generic source equality.
		return a.ReadScope(ctx, db, row.ActorID, row.ScopeKind, row.ScopeID)
	}
	if err != nil {
		return err
	}
	// Parent locks precede immutable source metadata and Sandbox. Stable order
	// serializes cross-scope restores without source/target ABBA locking.
	for _, before := range proof.scopes {
		after, err := a.lockScope(ctx, db, row.ActorID, before)
		if err != nil {
			return err
		}
		if after != before {
			return ErrWorkspaceJobConflict
		}
	}
	for _, before := range proof.sources {
		var current workspaceAuthorizationSource
		if err := workspaceCurrentRead(db).Table("workspace_snapshots").Select("id", "scope_kind", "scope_id", "actor_id", "job_id", "lineage", "source_binding_fingerprint").Take(&current, "id = ?", before.ID).Error; err != nil {
			return err
		}
		if before != current {
			return ErrWorkspaceJobConflict
		}
	}
	if err := a.checkGrantMetadata(db, proof.grant); err != nil {
		return err
	}
	binding, err := a.options.CurrentBinding(ctx, db, row.ActorID, row.ScopeKind, row.ScopeID, row.SandboxID)
	if err != nil {
		return err
	}
	if binding != proof.binding || !proof.expires.After(time.Now()) {
		return ErrWorkspaceJobConflict
	}
	resource, err := workspaceResource(db, row.SandboxID)
	if err != nil {
		return err
	}
	if resource != proof.resource {
		return ErrWorkspaceJobConflict
	}
	if inside {
		err = a.sandbox.guardWorkspaceReleaseTx(ctx, db, row.SandboxID)
	} else {
		err = a.sandbox.checkWorkspaceRelease(ctx, row.SandboxID)
	}
	if err != nil {
		return err
	}
	if !proof.expires.After(time.Now()) {
		return ErrWorkspaceJobAuthority
	}
	return nil
}

func (a *WorkspaceAuthorization) PrepareHold(ctx context.Context, id payloadhold.Identity) (context.Context, error) {
	var row models.WorkspaceSnapshotJob
	if err := a.db.WithContext(ctx).Take(&row, "id = ?", id.OperationID).Error; err != nil {
		return ctx, err
	}
	if err := holdJobIdentity(id, row); err != nil {
		return ctx, err
	}
	return a.PrepareJob(ctx, row)
}

func holdJobIdentity(id payloadhold.Identity, row models.WorkspaceSnapshotJob) error {
	var spec workspacefiles.ReadSpec
	if row.Kind != "read_snapshot" || row.ActorID != id.ActorID || row.ScopeKind != id.ScopeKind || row.ScopeID != id.ScopeID || row.Lineage != id.Lineage || !row.Deadline.UTC().Truncate(time.Millisecond).Equal(id.Deadline.UTC().Truncate(time.Millisecond)) || workspacefiles.Decode([]byte(row.Body), &spec) != nil || spec.SourceJobID != id.SnapshotID {
		return ErrWorkspaceJobConflict
	}
	return nil
}

func (a *WorkspaceAuthorization) AuthorizeHold(ctx context.Context, db *gorm.DB, id payloadhold.Identity) error {
	var row models.WorkspaceSnapshotJob
	if err := db.WithContext(ctx).Take(&row, "id = ?", id.OperationID).Error; err != nil {
		return err
	}
	if err := holdJobIdentity(id, row); err != nil {
		return err
	}
	return a.AuthorizeJob(ctx, db, row)
}

func (a *WorkspaceAuthorization) AuthorizeSnapshot(ctx context.Context, db *gorm.DB, actor string, snapshot models.WorkspaceSnapshot) error {
	return a.ReadScope(ctx, db, actor, snapshot.ScopeKind, snapshot.ScopeID)
}

func (a *WorkspaceAuthorization) ReadSnapshot(ctx context.Context, db *gorm.DB, actor string, snapshot models.WorkspaceSnapshot) error {
	return a.AuthorizeSnapshot(ctx, db, actor, snapshot)
}

func (a *WorkspaceAuthorization) CurrentRestoreBinding(ctx context.Context, db *gorm.DB, actor, solo string) (WorkspaceRestoreBinding, error) {
	var parent struct{ SandboxID uint }
	if err := a.ReadScope(ctx, db, actor, "solo", solo); err != nil {
		return WorkspaceRestoreBinding{}, err
	}
	if err := db.Table("solo_sessions").Select("sandbox_id").Take(&parent, "id = ? AND user_id = ?", solo, actor).Error; err != nil {
		return WorkspaceRestoreBinding{}, err
	}
	if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); !inside {
		return a.options.ResolveBinding(ctx, actor, "solo", solo, parent.SandboxID)
	}
	proof, ok := ctx.Value(workspaceAuthorizationKey{}).(workspaceAuthorizationProof)
	if !ok || proof.service != a || !proof.expires.After(time.Now()) || proof.resource.ID != parent.SandboxID || proof.resource.ThreadID != solo {
		return WorkspaceRestoreBinding{}, ErrWorkspaceJobAuthority
	}
	current, err := a.options.CurrentBinding(ctx, db, actor, "solo", solo, parent.SandboxID)
	if err != nil {
		return current, err
	}
	if current != proof.binding {
		return current, ErrWorkspaceJobConflict
	}
	return current, nil
}

func (a *WorkspaceAuthorization) RestoreOptions() WorkspaceRestoreOptions {
	return WorkspaceRestoreOptions{CurrentBinding: a.CurrentRestoreBinding, AuthorizeSnapshot: a.AuthorizeSnapshot, Prepare: a.execution.PrepareContext, GuardTx: func(ctx context.Context, tx *gorm.DB, row models.WorkspaceRestore) error {
		entities := []executionlineage.Entity{{Kind: executionlineage.EntityRestore, ID: row.ID}, {Kind: executionlineage.EntityWorkspaceJob, ID: row.JobID}, {Kind: executionlineage.EntitySolo, ID: row.SoloID}, {Kind: executionlineage.EntityQueue, ID: soloQueueScope(row.SoloID).ID()}}
		if row.TargetSandboxID == 0 {
			return ErrWorkspaceJobConflict
		}
		entities = append(entities, executionlineage.Entity{Kind: executionlineage.EntitySandbox, ID: strconv.FormatUint(uint64(row.TargetSandboxID), 10)})
		return a.execution.GuardContextTx(tx.Statement.Context, tx, entities...)
	}}
}

func (a *WorkspaceAuthorization) lockScope(ctx context.Context, db *gorm.DB, actor string, before workspaceAuthorizationScope) (workspaceAuthorizationScope, error) {
	if before.Kind != "run" {
		rows, err := a.readScope(ctx, db, actor, before.Kind, before.ID)
		if err != nil {
			return workspaceAuthorizationScope{}, err
		}
		return rows[len(rows)-1], nil
	}
	// Do not recursively lock OriginSolo here. All Run locks precede all Solo
	// locks in the sorted parent set, including cross-scope restores.
	var run struct {
		ID, OriginSoloID, WorkflowID, WorkspaceActorID string
		Attempt                                        int
	}
	if err := workspaceCurrentRead(db.WithContext(ctx)).Table("runs").Select("id", "origin_solo_id", "attempt", "workflow_id", "workspace_actor_id").Take(&run, "id = ?", before.ID).Error; err != nil {
		return workspaceAuthorizationScope{}, err
	}
	scope := workspaceAuthorizationScope{Kind: "run", ID: run.ID, OriginSolo: run.OriginSoloID, RunAttempt: run.Attempt}
	if strings.HasPrefix(actor, "api-key:") {
		if run.OriginSoloID != "" || run.WorkspaceActorID != actor {
			return scope, gorm.ErrRecordNotFound
		}
		var key struct{ ID, WorkflowID, KeyHash string }
		if err := workspaceCurrentRead(db.WithContext(ctx)).Table("workflow_api_keys").Select("id", "workflow_id", "key_hash").Where("id = ? AND workflow_id = ? AND revoked_at IS NULL", strings.TrimPrefix(actor, "api-key:"), run.WorkflowID).Take(&key).Error; err != nil {
			return scope, err
		}
		scope.APIKeyDigest = authorizationDigest(key)
	}
	return scope, nil
}
