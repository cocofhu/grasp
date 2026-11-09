package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const workspaceReleaseAction = "release_environment"

type WorkspaceReleaseService struct {
	db          *gorm.DB
	solo        *SoloService
	sbx         *SandboxService
	options     WorkspaceReleaseOptions
	wake        chan struct{}
	admissionMu sync.Mutex
	paused      bool
	operations  int64
}
type workspaceReleaseSnapshot struct{ ID, ArchiveSHA256, ManifestSHA256 string }
type workspaceReleaseScope struct {
	Bindings  WorkspaceReleaseBindingScope
	Snapshots []workspaceReleaseSnapshot
}

func NewWorkspaceReleaseService(db *gorm.DB, solo *SoloService, sbx *SandboxService, options WorkspaceReleaseOptions) (*WorkspaceReleaseService, error) {
	if db == nil || solo == nil || solo.durable == nil || sbx == nil || options.Lineage == "" || len(options.Lineage) > 128 || options.Prepare == nil || options.GuardTx == nil || options.Bindings == nil {
		return nil, ErrWorkspaceReleaseUnsupported
	}
	if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); inside {
		return nil, ErrWorkspaceReleaseInvalid
	}
	if options.MaxDuration <= 0 {
		options.MaxDuration = 5 * time.Minute
	}
	if options.MaxDuration > 30*time.Minute {
		return nil, ErrWorkspaceReleaseInvalid
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	if options.Provider == nil {
		options.Provider = &gatewayWorkspaceReleaseProvider{service: sbx}
	}
	return &WorkspaceReleaseService{db: db, solo: solo, sbx: sbx, options: options, wake: make(chan struct{}, 1)}, nil
}
func workspaceReleaseIdentity(actor, solo, nonce string) string {
	sum := sha256.Sum256([]byte(actor + "\x00" + solo + "\x00" + nonce))
	return hex.EncodeToString(sum[:])
}
func releaseScopeJSON(scope workspaceReleaseScope) (string, error) {
	sort.Slice(scope.Snapshots, func(i, j int) bool { return scope.Snapshots[i].ID < scope.Snapshots[j].ID })
	sort.Strings(scope.Bindings.SnapshotIDs)
	sort.Strings(scope.Bindings.Blockers)
	body, err := json.Marshal(scope)
	if err != nil || len(body) > 64<<10 {
		return "", ErrWorkspaceReleaseInvalid
	}
	return string(body), nil
}
func workspaceReleaseView(row models.WorkspaceRelease) (WorkspaceReleaseView, error) {
	var scope workspaceReleaseScope
	if json.Unmarshal([]byte(row.ScopeJSON), &scope) != nil {
		return WorkspaceReleaseView{}, ErrWorkspaceReleaseConflict
	}
	snapshots := map[string]bool{}
	for _, x := range scope.Snapshots {
		snapshots[x.ID] = true
	}
	for _, id := range scope.Bindings.SnapshotIDs {
		snapshots[id] = true
	}
	return WorkspaceReleaseView{ID: row.ID, State: row.State, WholeEnvironment: true, ChatsRetained: true, SnapshotsRetained: len(snapshots), WorkspacesAffected: scope.Bindings.Bindings, ReturnPointsRetained: scope.Bindings.ReturnPoints, LossConfirmationRequired: true, RequiresReconciliation: row.State == "unknown" || row.State == "dispatching", Blockers: scope.Bindings.Blockers, Error: row.Error, CreatedAt: row.CreatedAt}, nil
}
func (s *WorkspaceReleaseService) load(ctx context.Context, actor, solo, id string) (models.WorkspaceRelease, error) {
	var row models.WorkspaceRelease
	if actor == "" || solo == "" || !workspaceJobID.MatchString(id) {
		return row, gorm.ErrRecordNotFound
	}
	var parent models.SoloSession
	if err := s.db.WithContext(ctx).Select("id").Take(&parent, "id = ? AND user_id = ?", solo, actor).Error; err != nil {
		return row, err
	}
	err := s.db.WithContext(ctx).Where("id = ? AND solo_id = ? AND actor_id = ?", id, solo, actor).Take(&row).Error
	return row, err
}
func (s *WorkspaceReleaseService) Get(ctx context.Context, actor, solo, id string) (WorkspaceReleaseView, error) {
	row, err := s.load(ctx, actor, solo, id)
	if err != nil {
		return WorkspaceReleaseView{}, err
	}
	return workspaceReleaseView(row)
}
func (s *WorkspaceReleaseService) List(ctx context.Context, actor, solo string) ([]WorkspaceReleaseView, error) {
	if _, err := s.solo.Require(solo, actor); err != nil {
		return nil, err
	}
	var rows []models.WorkspaceRelease
	if err := s.db.WithContext(ctx).Where("solo_id = ? AND actor_id = ?", solo, actor).Order("created_at desc,id").Limit(50).Find(&rows).Error; err != nil {
		return nil, err
	}
	views := make([]WorkspaceReleaseView, 0, len(rows))
	for _, row := range rows {
		view, err := workspaceReleaseView(row)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}
func (s *WorkspaceReleaseService) privateContext(ctx context.Context, row models.WorkspaceRelease) context.Context {
	return context.WithValue(ctx, workspaceReleaseCapabilityKey{}, workspaceReleaseCapability{row.ID, row.SandboxID})
}
func (s *WorkspaceReleaseService) transaction(ctx context.Context, row models.WorkspaceRelease, body func(*gorm.DB) error) error {
	prepared, err := s.options.Prepare(ctx)
	if err != nil {
		return err
	}
	return s.db.WithContext(prepared).Transaction(func(tx *gorm.DB) error {
		if err := s.options.GuardTx(prepared, tx, row); err != nil {
			return err
		}
		if err := body(tx); err != nil {
			return err
		}
		return s.options.GuardTx(prepared, tx, row)
	})
}

// prepareScope is SQL-only and locks compute -> queue -> parent -> binding
// metadata. commitSandboxOperationPrepared takes the physical row afterward.
func (s *WorkspaceReleaseService) prepareScope(ctx context.Context, tx *gorm.DB, row models.WorkspaceRelease, accepted bool, deferConsumers ...bool) (workspaceReleaseScope, error) {
	var scope workspaceReleaseScope
	if err := lockSoloCompute(tx); err != nil {
		return scope, err
	}
	// GuardIdle locks the admission row even for an already-retired queue.
	// Do not call Retire here: that would cancel accepted work before checking
	// it and incorrectly convert a busy environment into a releasable scope.
	if err := s.solo.durable.GuardIdle(tx, soloQueueScope(row.SoloID)); err != nil && !errors.Is(err, chatsession.ErrQueueRetired) {
		if errors.Is(err, chatsession.ErrQueueBusy) {
			return scope, ErrWorkspaceReleaseBlocked
		}
		return scope, err
	}
	var queue models.ChatQueue
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&queue, "id = ?", soloQueueScope(row.SoloID).ID()).Error; err != nil {
		return scope, err
	}
	var active int64
	if err := tx.Model(&models.ChatQueueTurn{}).Where("queue_id = ? AND state IN ?", queue.ID, []string{chatsession.TurnQueued, chatsession.TurnExecuting}).Count(&active).Error; err != nil {
		return scope, err
	}
	if queue.ActiveTurnID != "" || active != 0 {
		return scope, ErrWorkspaceReleaseBlocked
	}
	var parent models.SoloSession
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&parent, "id = ? AND user_id = ?", row.SoloID, row.ActorID).Error; err != nil {
		return scope, err
	}
	expectedAction := ""
	if accepted {
		expectedAction = workspaceReleaseAction
	}
	if parent.Status != models.SoloStatusClosed || parent.SandboxID != row.SandboxID || parent.LifecycleEpoch != row.LifecycleEpoch || parent.LifecycleAttemptID != row.LifecycleAttempt || parent.LifecycleAction != expectedAction {
		return scope, ErrWorkspaceReleaseConflict
	}
	var err error
	scope.Bindings, err = s.options.Bindings.ReleaseScope(ctx, tx, row.SoloID, row.SandboxID)
	if err != nil {
		return scope, err
	}
	if scope.Bindings.Fingerprint == "" || scope.Bindings.Bindings < 0 || scope.Bindings.ReturnPoints < 0 {
		return scope, ErrWorkspaceReleaseConflict
	}
	for _, code := range scope.Bindings.Blockers {
		if code == "workspace_consumers_busy" && len(deferConsumers) > 0 && deferConsumers[0] {
			continue
		}
		return scope, ErrWorkspaceReleaseBlocked
	}
	var jobs int64
	if err := tx.Model(&models.WorkspaceSnapshotJob{}).Where("sandbox_id = ? AND state NOT IN ?", row.SandboxID, []string{"executed", "failed", "unsupported", "cancelled"}).Count(&jobs).Error; err != nil {
		return scope, err
	}
	if jobs != 0 {
		return scope, ErrWorkspaceReleaseBlocked
	}
	var restores int64
	if err := tx.Model(&models.WorkspaceRestore{}).Where("target_sandbox_id = ? AND state <> ?", row.SandboxID, "restored").Count(&restores).Error; err != nil {
		return scope, err
	}
	if restores != 0 {
		return scope, ErrWorkspaceReleaseBlocked
	}
	var snapshots []models.WorkspaceSnapshot
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "archive_sha256", "manifest_sha256", "state", "verified_at", "actor_id", "scope_kind", "scope_id").Where("source_sandbox_id = ?", row.SandboxID).Order("id").Find(&snapshots).Error; err != nil {
		return scope, err
	}
	for _, snapshot := range snapshots {
		if snapshot.State == "capturing" || snapshot.State == "captured" || snapshot.State == "publishing" {
			return scope, ErrWorkspaceReleaseBlocked
		}
		if snapshot.State == "ready" && snapshot.VerifiedAt != nil && snapshot.ActorID == row.ActorID && snapshot.ScopeKind == "solo" && snapshot.ScopeID == row.SoloID {
			scope.Snapshots = append(scope.Snapshots, workspaceReleaseSnapshot{snapshot.ID, snapshot.ArchiveSHA256, snapshot.ManifestSHA256})
		}
	}
	return scope, nil
}
func (s *WorkspaceReleaseService) physical(tx *gorm.DB, row models.WorkspaceRelease) error {
	var source models.Sandbox
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "name", "thread_id", "purpose", "status", "desired_state").Take(&source, row.SandboxID).Error; err != nil {
		return err
	}
	if source.Name != row.Resource || source.ThreadID != row.SoloID || source.Purpose != SandboxPurposeSolo || source.Status != "stopped" || source.DesiredState != "stopped" {
		return ErrWorkspaceReleaseConflict
	}
	return nil
}
func (s *WorkspaceReleaseService) Preview(ctx context.Context, actor, solo string, in WorkspaceReleasePreviewInput) (WorkspaceReleaseView, error) {
	if err := s.beginRelease(); err != nil {
		return WorkspaceReleaseView{}, err
	}
	defer s.finishRelease()
	if !workspaceJobID.MatchString(in.ClientRequestID) {
		return WorkspaceReleaseView{}, ErrWorkspaceReleaseInvalid
	}
	id := workspaceReleaseIdentity(actor, solo, in.ClientRequestID)
	if prior, err := s.load(ctx, actor, solo, id); err == nil {
		return workspaceReleaseView(prior)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return WorkspaceReleaseView{}, err
	}
	parent, err := s.solo.Require(solo, actor)
	if err != nil {
		return WorkspaceReleaseView{}, err
	}
	if parent.Status != models.SoloStatusClosed || parent.SandboxID == 0 || parent.LifecycleAction != "" {
		return WorkspaceReleaseView{}, ErrWorkspaceReleaseBlocked
	}
	row := models.WorkspaceRelease{ID: id, ActorID: actor, SoloID: solo, SandboxID: parent.SandboxID, Lineage: s.options.Lineage, PreviewNonce: in.ClientRequestID, LifecycleEpoch: parent.LifecycleEpoch, LifecycleAttempt: parent.LifecycleAttemptID, State: "prepared", Deadline: time.Now().UTC().Add(10 * time.Minute)}
	var source models.Sandbox
	if err := s.db.WithContext(ctx).Select("id", "name").Take(&source, parent.SandboxID).Error; err != nil {
		return WorkspaceReleaseView{}, err
	}
	row.Resource = source.Name
	// Fail before holding a physical lease when another durable operation/pin
	// already owns the environment. No body/object I/O occurs in this TX.
	if err := s.transaction(ctx, row, func(tx *gorm.DB) error {
		_, err := s.prepareScope(tx.Statement.Context, tx, row, false, true)
		if err != nil {
			return err
		}
		return s.physical(tx, row)
	}); err != nil {
		return WorkspaceReleaseView{}, err
	}
	operation, release, err := s.sbx.acquireSandboxOperation(ctx, row.SandboxID, "release_preview")
	if err != nil {
		return WorkspaceReleaseView{}, err
	}
	defer release()
	caps, err := s.options.Provider.Capabilities(operation, row.Resource)
	if err != nil {
		return WorkspaceReleaseView{}, err
	}
	if !caps.Supported || caps.Version != sandbox.EnvironmentReleaseVersion {
		return WorkspaceReleaseView{}, workspaceReleaseUnsupported(caps.Reason)
	}
	view, err := s.options.Provider.Preview(operation, row.Resource, sandbox.EnvironmentReleasePrepare{ID: row.ID, Lineage: row.Lineage})
	if err != nil {
		return WorkspaceReleaseView{}, err
	}
	if !validProviderReleasePlan(view, row.ID) {
		return WorkspaceReleaseView{}, ErrWorkspaceReleaseConflict
	}
	plan, _ := json.Marshal(view)
	row.ProviderPlan, row.ProviderPlanSHA256 = string(plan), view.Plan.SHA256
	prepared, err := s.options.Prepare(operation)
	if err != nil {
		return WorkspaceReleaseView{}, err
	}
	var scope workspaceReleaseScope
	err = s.sbx.commitSandboxOperationPrepared(prepared, row.SandboxID, func(tx *gorm.DB) error {
		if err := s.options.GuardTx(tx.Statement.Context, tx, row); err != nil {
			return err
		}
		// Acquire compute/queue/parent/binding locks before closing tracked
		// consumers. Only the provider's fresh stopped observation permits it.
		if _, err := s.prepareScope(tx.Statement.Context, tx, row, false, true); err != nil {
			return err
		}
		if stopped, ok := s.options.Bindings.(interface {
			EnvironmentStoppedTx(context.Context, *gorm.DB, uint) error
		}); ok {
			if err := stopped.EnvironmentStoppedTx(tx.Statement.Context, tx, row.SandboxID); err != nil {
				return err
			}
		}
		var err error
		scope, err = s.prepareScope(tx.Statement.Context, tx, row, false)
		return err
	}, func(tx *gorm.DB) error {
		if err := s.physical(tx, row); err != nil {
			return err
		}
		var err error
		row.ScopeJSON, err = releaseScopeJSON(scope)
		if err != nil {
			return err
		}
		row.BindingsFingerprint = scope.Bindings.Fingerprint
		sum := sha256.Sum256([]byte(row.Resource + "\x00" + row.Lineage + "\x00" + row.ScopeJSON + "\x00" + row.ProviderPlanSHA256))
		row.Fingerprint = hex.EncodeToString(sum[:])
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		return result.Error
	})
	if err != nil {
		return WorkspaceReleaseView{}, err
	}
	return s.Get(ctx, actor, solo, row.ID)
}
func validProviderReleasePlan(view sandbox.EnvironmentReleaseView, id string) bool {
	return view.ID == id && view.Plan.Valid()
}
func (s *WorkspaceReleaseService) Confirm(ctx context.Context, actor, solo, id string, in WorkspaceReleaseConfirmInput) (WorkspaceReleaseView, error) {
	if err := s.beginRelease(); err != nil {
		return WorkspaceReleaseView{}, err
	}
	defer s.finishRelease()
	if !in.Confirmed || !in.LossAccepted || !workspaceJobID.MatchString(in.ClientRequestID) {
		return WorkspaceReleaseView{}, ErrWorkspaceReleaseInvalid
	}
	row, err := s.load(ctx, actor, solo, id)
	if err != nil {
		return WorkspaceReleaseView{}, err
	}
	if row.ConfirmNonce != "" {
		if row.ConfirmNonce != in.ClientRequestID {
			return WorkspaceReleaseView{}, ErrWorkspaceReleaseConflict
		}
		return workspaceReleaseView(row)
	}
	if row.State != "prepared" || !row.Deadline.After(time.Now()) {
		return WorkspaceReleaseView{}, ErrWorkspaceReleaseConflict
	}
	operation, release, err := s.sbx.acquireSandboxOperation(ctx, row.SandboxID, "release_confirm")
	if err != nil {
		if current, readErr := s.load(ctx, actor, solo, id); readErr == nil && current.ConfirmNonce == in.ClientRequestID {
			return workspaceReleaseView(current)
		}
		return WorkspaceReleaseView{}, err
	}
	defer release()
	current, err := s.load(operation, actor, solo, id)
	if err != nil {
		return WorkspaceReleaseView{}, err
	}
	if current.ConfirmNonce != "" {
		if current.ConfirmNonce != in.ClientRequestID {
			return WorkspaceReleaseView{}, ErrWorkspaceReleaseConflict
		}
		return workspaceReleaseView(current)
	}
	prepared, err := s.options.Prepare(operation)
	if err != nil {
		return WorkspaceReleaseView{}, err
	}
	err = s.sbx.commitSandboxOperationPrepared(prepared, row.SandboxID, func(tx *gorm.DB) error {
		if err := s.options.GuardTx(tx.Statement.Context, tx, row); err != nil {
			return err
		}
		scope, err := s.prepareScope(tx.Statement.Context, tx, row, false)
		if err != nil {
			return err
		}
		encoded, err := releaseScopeJSON(scope)
		if err != nil || encoded != row.ScopeJSON || scope.Bindings.Fingerprint != row.BindingsFingerprint {
			return ErrWorkspaceReleaseConflict
		}
		return nil
	}, func(tx *gorm.DB) error {
		if err := s.physical(tx, row); err != nil {
			return err
		}
		now := time.Now().UTC()
		attempt := row.ID[:32]
		changed := tx.Model(&models.SoloSession{}).Where("id = ? AND user_id = ? AND sandbox_id = ? AND status = ? AND lifecycle_epoch = ? AND lifecycle_attempt_id = ? AND lifecycle_action = ''", solo, actor, row.SandboxID, models.SoloStatusClosed, row.LifecycleEpoch, row.LifecycleAttempt).Updates(map[string]any{"lifecycle_epoch": row.LifecycleEpoch + 1, "lifecycle_attempt_id": attempt, "lifecycle_action": workspaceReleaseAction, "lifecycle_source_sandbox_id": row.SandboxID})
		if changed.Error != nil {
			return changed.Error
		}
		if changed.RowsAffected != 1 {
			return ErrWorkspaceReleaseConflict
		}
		accepted := tx.Model(&models.WorkspaceRelease{}).Where("id = ? AND state = 'prepared' AND confirm_nonce = '' AND deadline > ?", row.ID, now).Updates(map[string]any{"state": "confirmed", "confirm_nonce": in.ClientRequestID, "lifecycle_epoch": row.LifecycleEpoch + 1, "lifecycle_attempt": attempt, "deadline": now.Add(s.options.MaxDuration)})
		if accepted.Error != nil {
			return accepted.Error
		}
		if accepted.RowsAffected != 1 {
			return ErrWorkspaceReleaseConflict
		}
		return s.options.GuardTx(tx.Statement.Context, tx, row)
	})
	if err != nil {
		return WorkspaceReleaseView{}, err
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return s.Get(ctx, actor, solo, id)
}

type gatewayWorkspaceReleaseProvider struct{ service *SandboxService }

func (p *gatewayWorkspaceReleaseProvider) gateway() (*sandbox.GatewayClient, error) {
	if p.service.mgr == nil || p.service.mgr.Gateway() == nil {
		return nil, ErrWorkspaceReleaseUnsupported
	}
	return p.service.mgr.Gateway(), nil
}
func (p *gatewayWorkspaceReleaseProvider) Capabilities(ctx context.Context, id string) (sandbox.EnvironmentReleaseCapabilities, error) {
	g, err := p.gateway()
	if err != nil {
		return sandbox.EnvironmentReleaseCapabilities{Reason: "provider_control_unavailable"}, nil
	}
	caps, err := g.EnvironmentReleaseCapabilities(ctx, id)
	var httpErr *sandbox.GatewayHTTPError
	if errors.As(err, &httpErr) && (httpErr.StatusCode == 404 || httpErr.StatusCode == 501) {
		return sandbox.EnvironmentReleaseCapabilities{Reason: "gateway_protocol_unavailable"}, nil
	}
	return caps, err
}
func (p *gatewayWorkspaceReleaseProvider) Preview(ctx context.Context, id string, in sandbox.EnvironmentReleasePrepare) (sandbox.EnvironmentReleaseView, error) {
	g, err := p.gateway()
	if err != nil {
		return sandbox.EnvironmentReleaseView{}, err
	}
	view, err := g.PreviewEnvironmentRelease(ctx, id, in)
	var status *sandbox.GatewayHTTPError
	if errors.As(err, &status) && status.StatusCode == 501 {
		return view, workspaceReleaseUnsupported("physical_storage_unsupported")
	}
	return view, err
}
func (p *gatewayWorkspaceReleaseProvider) Confirm(ctx context.Context, id, op string, in sandbox.EnvironmentReleaseConfirm) (sandbox.EnvironmentReleaseView, error) {
	g, err := p.gateway()
	if err != nil {
		return sandbox.EnvironmentReleaseView{}, err
	}
	return g.ConfirmEnvironmentRelease(ctx, id, op, in)
}
func (p *gatewayWorkspaceReleaseProvider) Renew(ctx context.Context, id, op string, in sandbox.EnvironmentReleaseRenew) (sandbox.EnvironmentReleaseView, error) {
	g, err := p.gateway()
	if err != nil {
		return sandbox.EnvironmentReleaseView{}, err
	}
	return g.RenewEnvironmentRelease(ctx, id, op, in)
}
func (p *gatewayWorkspaceReleaseProvider) Get(ctx context.Context, id, op string) (sandbox.EnvironmentReleaseView, error) {
	g, err := p.gateway()
	if err != nil {
		return sandbox.EnvironmentReleaseView{}, err
	}
	return g.GetEnvironmentRelease(ctx, id, op)
}
