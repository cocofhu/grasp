package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

type WorkspaceBindingRemote interface {
	WorkspaceBindingCapabilities(context.Context) (sandbox.WorkspaceBindingCapabilities, error)
	WorkspaceBindingHead(context.Context) (sandbox.WorkspaceBindingView, error)
	WorkspaceBindingOperation(context.Context, string) (sandbox.WorkspaceBindingOperation, error)
}

type WorkspaceBindingOptions struct {
	ExpectedLineage       string
	FileOperationsEnabled bool
	Prepare               func(context.Context) (context.Context, error)
	ReadScope             func(context.Context, *gorm.DB, string, string, string) error
	CurrentAuthorization  func(context.Context, string, string, string, uint) (string, error)
	GuardTx               func(context.Context, *gorm.DB, string, string, uint) error
	Remote                func(context.Context, uint) (WorkspaceBindingRemote, error)
}

// WorkspaceActiveBinding contains private control-plane proof. Product DTOs
// expose the logical references, never Path, host addresses or credentials.
type WorkspaceActiveBinding struct {
	SandboxID                       uint
	Resource, Lineage               string
	Reference                       sandbox.WorkspaceBindingReference
	Fingerprint                     string
	LifecycleEpoch                  uint64
	LifecycleAttempt, Authorization string
	Path                            string
}

type WorkspaceBindingService struct {
	db         *gorm.DB
	sandbox    *SandboxService
	options    WorkspaceBindingOptions
	operations atomic.Pointer[workspaceBindingOperations]
	jobHooks   atomic.Pointer[workspaceBindingJobHooks]
}

func NewWorkspaceBindingService(db *gorm.DB, sbx *SandboxService, options WorkspaceBindingOptions) (*WorkspaceBindingService, error) {
	if db == nil || sbx == nil || !workspaceJobID.MatchString(options.ExpectedLineage) || options.Prepare == nil || options.ReadScope == nil || options.CurrentAuthorization == nil || options.GuardTx == nil || messageOutsideTransaction(db) != nil {
		return nil, ErrWorkspaceJobAuthority
	}
	if options.Remote == nil {
		options.Remote = func(ctx context.Context, id uint) (WorkspaceBindingRemote, error) {
			row, err := sbx.Get(id)
			if err != nil {
				return nil, err
			}
			host, port, password, err := sbx.acpConnectionFromRow(ctx, row, false)
			if err != nil {
				return nil, err
			}
			return sandbox.WorkspaceJobClient{Host: host, Port: port, Password: password}, nil
		}
	}
	return &WorkspaceBindingService{db: db, sandbox: sbx, options: options}, nil
}

func (s *SandboxService) SetWorkspaceBindings(bindings *WorkspaceBindingService) {
	s.workspaceBindings.Store(bindings)
}

type bindingParent struct {
	Kind, ID, Owner, Status, Action, Attempt string
	SandboxID                                uint
	Epoch                                    uint64
	RunAttempt                               int
}

func (s *WorkspaceBindingService) parent(ctx context.Context, db *gorm.DB, actor, kind, id string, sandboxID uint) (bindingParent, error) {
	var parent bindingParent
	if err := s.options.ReadScope(ctx, db, actor, kind, id); err != nil {
		return parent, err
	}
	if kind == "solo" {
		var row struct {
			ID, UserID, Status, LifecycleAction, LifecycleAttemptID string
			SandboxID                                               uint
			LifecycleEpoch                                          uint64
		}
		if err := workspaceCurrentRead(db.WithContext(ctx)).Table("solo_sessions").Select("id", "user_id", "sandbox_id", "status", "lifecycle_epoch", "lifecycle_attempt_id", "lifecycle_action").Take(&row, "id = ? AND user_id = ?", id, actor).Error; err != nil {
			return parent, err
		}
		if row.SandboxID != sandboxID || row.Status != models.SoloStatusRunning || row.LifecycleAction != "" {
			return parent, ErrWorkspaceJobConflict
		}
		return bindingParent{Kind: kind, ID: id, Owner: actor, Status: row.Status, Action: row.LifecycleAction, Attempt: row.LifecycleAttemptID, SandboxID: sandboxID, Epoch: row.LifecycleEpoch}, nil
	}
	if kind != "run" {
		return parent, gorm.ErrRecordNotFound
	}
	var row struct {
		ID, Status string
		Attempt    int
	}
	if err := workspaceCurrentRead(db.WithContext(ctx)).Table("runs").Select("id", "status", "attempt").Take(&row, "id = ?", id).Error; err != nil {
		return parent, err
	}
	return bindingParent{Kind: kind, ID: id, Status: row.Status, SandboxID: sandboxID, RunAttempt: row.Attempt}, nil
}

// Only immutable environment identity enters the logical hash. Every lease
// claim may increment Sandbox.Generation, which must not change this value.
type bindingResource struct {
	ID                                                                                              uint
	Name, Profile, Purpose, ThreadID, RunID, NodeID, Status, DesiredState, AuthorizationFingerprint string
	StateRunID                                                                                      uint
	Iteration, RunAttempt, ProviderAttempt                                                          int
}

func bindingResourceRead(db *gorm.DB, id uint) (bindingResource, error) {
	var row bindingResource
	err := workspaceCurrentRead(db).Table("sandboxes").Select("id", "name", "profile", "purpose", "thread_id", "run_id", "node_id", "state_run_id", "iteration", "run_attempt", "provider_attempt", "status", "desired_state", "authorization_fingerprint").Take(&row, id).Error
	return row, err
}
func validBindingResource(row bindingResource, parent bindingParent) bool {
	if row.ID != parent.SandboxID || row.Name == "" || row.Status != "running" || (row.DesiredState != "" && row.DesiredState != "running") {
		return false
	}
	if parent.Kind == "solo" {
		return row.Purpose == SandboxPurposeSolo && row.ThreadID == parent.ID
	}
	return row.Purpose == "run" && row.RunID == parent.ID && row.StateRunID > 0 && row.NodeID != "" && row.Iteration > 0
}
func bindingDigest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func validNativeBinding(head sandbox.WorkspaceBindingHead, resource, lineage string) bool {
	return sandbox.ValidWorkspaceBindingHead(head, resource, lineage)
}
func bindingAggregate(parent bindingParent, resource bindingResource, head sandbox.WorkspaceBindingHead, authorization string) string {
	return bindingDigest(struct {
		Parent        bindingParent
		Resource      bindingResource
		Head          sandbox.WorkspaceBindingHead
		Authorization string
	}{parent, resource, head, authorization})
}
func bindingHistoryID(sandboxID uint, nativeID string) string {
	return bindingDigest(struct {
		SandboxID uint
		NativeID  string
	}{sandboxID, nativeID})
}

func (s *WorkspaceBindingService) Resolve(ctx context.Context, actor, soloID string) (WorkspaceActiveBinding, error) {
	var row struct{ SandboxID uint }
	if err := s.options.ReadScope(ctx, s.db, actor, "solo", soloID); err != nil {
		return WorkspaceActiveBinding{}, err
	}
	if err := s.db.WithContext(ctx).Table("solo_sessions").Select("sandbox_id").Take(&row, "id = ? AND user_id = ?", soloID, actor).Error; err != nil {
		return WorkspaceActiveBinding{}, err
	}
	return s.ResolveSandbox(ctx, actor, "solo", soloID, row.SandboxID)
}

// ResolveSandbox performs authenticated native reads outside SQL. Initializing
// an original head is a metadata operation; it never wakes or recreates a guest.
// A stored authorization hash is not execution permission: job admission and
// publication must additionally use the strict current-grant factory proof.
func (s *WorkspaceBindingService) ResolveSandbox(ctx context.Context, actor, kind, id string, sandboxID uint) (WorkspaceActiveBinding, error) {
	var zero WorkspaceActiveBinding
	if proof, ok := ctx.Value(workspaceBindingAcceptedKey{}).(workspaceBindingAccepted); ok && proof.service == s {
		return s.acceptedBinding(ctx, s.db, actor, kind, id, sandboxID, proof)
	}
	if actor == "" || sandboxID == 0 || messageOutsideTransaction(s.db) != nil {
		return zero, ErrWorkspaceJobInvalid
	}
	ctx, err := s.options.Prepare(ctx)
	if err != nil {
		return zero, err
	}
	parent, err := s.parent(ctx, s.db, actor, kind, id, sandboxID)
	if err != nil {
		return zero, err
	}
	resource, err := bindingResourceRead(s.db.WithContext(ctx), sandboxID)
	if err != nil {
		return zero, err
	}
	if !validBindingResource(resource, parent) {
		return zero, ErrWorkspaceJobUnsupported
	}
	authorization, err := s.options.CurrentAuthorization(ctx, actor, kind, id, sandboxID)
	if err != nil {
		return zero, err
	}
	if !validDigest(authorization) {
		return zero, ErrWorkspaceJobAuthority
	}
	remote, err := s.options.Remote(ctx, sandboxID)
	if err != nil {
		return zero, err
	}
	caps, err := remote.WorkspaceBindingCapabilities(ctx)
	if err != nil {
		return zero, err
	}
	if caps.Version != 1 || caps.Resource != resource.Name || caps.Lineage != s.options.ExpectedLineage || !caps.DurableHead || !caps.Activate || !caps.Return || !caps.PortOwnership || caps.ProcessMemory {
		return zero, ErrWorkspaceJobUnsupported
	}
	view, err := remote.WorkspaceBindingHead(ctx)
	if err != nil {
		return zero, err
	}
	if !view.Ready || view.PendingOperationID != "" {
		return zero, ErrWorkspaceJobUnknown
	}
	if !validNativeBinding(view.Head, resource.Name, caps.Lineage) || !filepath.IsAbs(view.Path) || strings.ContainsRune(view.Path, 0) {
		return zero, ErrWorkspaceJobConflict
	}
	fingerprint := bindingAggregate(parent, resource, view.Head, authorization)
	value := WorkspaceActiveBinding{SandboxID: sandboxID, Resource: resource.Name, Lineage: caps.Lineage, Reference: view.Head.Reference, Fingerprint: fingerprint, LifecycleEpoch: parent.Epoch, LifecycleAttempt: parent.Attempt, Authorization: authorization, Path: view.Path}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := s.parent(ctx, tx, actor, kind, id, sandboxID)
		if err != nil {
			return err
		}
		if current != parent {
			return ErrWorkspaceJobConflict
		}
		var prior models.WorkspaceBindingHead
		found := workspaceCurrentRead(tx).Take(&prior, "sandbox_id = ?", sandboxID).Error
		if found != nil && !errors.Is(found, gorm.ErrRecordNotFound) {
			return found
		}
		if found == nil && (prior.State == "released" || prior.PendingOperationID != "") {
			return ErrWorkspaceJobUnknown
		}
		// A different native head is not adopted by a read. Only the original
		// operation's verified completion may advance a published logical root.
		if found == nil && (prior.NativeID != view.Head.Reference.ID || prior.Revision != view.Head.Reference.Revision || prior.NativeFingerprint != view.Head.Reference.Fingerprint || prior.Resource != resource.Name || prior.Lineage != caps.Lineage) {
			return ErrWorkspaceJobUnknown
		}
		if errors.Is(found, gorm.ErrRecordNotFound) && (view.Head.Reference.Revision != 1 || view.Head.Target.Kind != "original") {
			return ErrWorkspaceJobUnknown
		}
		actual, err := bindingResourceRead(tx, sandboxID)
		if err != nil {
			return err
		}
		if actual != resource {
			return ErrWorkspaceJobConflict
		}
		if err := s.options.GuardTx(ctx, tx, kind, id, sandboxID); err != nil {
			return err
		}
		native, _ := json.Marshal(view.Head)
		head := models.WorkspaceBindingHead{SandboxID: sandboxID, ScopeKind: kind, ScopeID: id, Resource: resource.Name, Lineage: caps.Lineage, BindingID: bindingHistoryID(sandboxID, view.Head.Reference.ID), NativeID: view.Head.Reference.ID, Revision: view.Head.Reference.Revision, NativeFingerprint: view.Head.Reference.Fingerprint, Fingerprint: fingerprint, NativeHead: string(native), AuthorizationFingerprint: authorization, LifecycleEpoch: parent.Epoch, LifecycleAttempt: parent.Attempt, State: "ready"}
		if errors.Is(found, gorm.ErrRecordNotFound) {
			if err := tx.Create(&head).Error; err != nil {
				return err
			}
			history := models.WorkspaceBinding{ID: head.BindingID, ActorID: actor, SandboxID: sandboxID, Resource: resource.Name, Lineage: caps.Lineage, NativeID: head.NativeID, Revision: head.Revision, NativeFingerprint: head.NativeFingerprint, NativeHead: head.NativeHead, SelectorKind: view.Head.Target.Kind, SelectorID: view.Head.Target.ID, State: "active"}
			if kind == "solo" {
				history.SoloID = id
			}
			return tx.Create(&history).Error
		}
		return tx.Model(&models.WorkspaceBindingHead{}).Where("sandbox_id = ? AND binding_id = ? AND pending_operation_id = '' AND state = ?", sandboxID, prior.BindingID, "ready").Updates(map[string]any{"fingerprint": fingerprint, "authorization_fingerprint": authorization, "lifecycle_epoch": parent.Epoch, "lifecycle_attempt": parent.Attempt}).Error
	})
	if err != nil {
		return zero, err
	}
	s.sandbox.mu.Lock()
	live := s.sandbox.live[sandboxID]
	if live != nil && live.sb != nil {
		live.sb.SetActiveWorkspace(view.Path, view.Head.Reference.Fingerprint)
	}
	s.sandbox.mu.Unlock()
	return value, nil
}

// CurrentSandboxBinding is a pure metadata current read. It never initializes
// a head or probes a provider, even when called in the outer SQL transaction.
func (s *WorkspaceBindingService) CurrentSandboxBinding(ctx context.Context, db *gorm.DB, actor, kind, id string, sandboxID uint) (WorkspaceRestoreBinding, error) {
	var zero WorkspaceRestoreBinding
	if proof, ok := ctx.Value(workspaceBindingAcceptedKey{}).(workspaceBindingAccepted); ok && proof.service == s {
		value, err := s.acceptedBinding(ctx, db, actor, kind, id, sandboxID, proof)
		return WorkspaceRestoreBinding{SandboxID: value.SandboxID, Fingerprint: value.Fingerprint}, err
	}
	parent, err := s.parent(ctx, db, actor, kind, id, sandboxID)
	if err != nil {
		return zero, err
	}
	var head models.WorkspaceBindingHead
	if err := workspaceCurrentRead(db).Take(&head, "sandbox_id = ?", sandboxID).Error; err != nil {
		return zero, err
	}
	if head.State != "ready" || head.PendingOperationID != "" || head.ScopeKind != kind || head.ScopeID != id || head.LifecycleEpoch != parent.Epoch || head.LifecycleAttempt != parent.Attempt {
		return zero, ErrWorkspaceJobConflict
	}
	resource, err := bindingResourceRead(db, sandboxID)
	if err != nil {
		return zero, err
	}
	if !validBindingResource(resource, parent) || resource.Name != head.Resource {
		return zero, ErrWorkspaceJobConflict
	}
	var native sandbox.WorkspaceBindingHead
	if json.Unmarshal([]byte(head.NativeHead), &native) != nil || !validNativeBinding(native, head.Resource, head.Lineage) || native.Reference.ID != head.NativeID || native.Reference.Revision != head.Revision || native.Reference.Fingerprint != head.NativeFingerprint || head.Fingerprint != bindingAggregate(parent, resource, native, head.AuthorizationFingerprint) {
		return zero, ErrWorkspaceJobConflict
	}
	return WorkspaceRestoreBinding{SandboxID: sandboxID, Fingerprint: head.Fingerprint}, nil
}
func (s *WorkspaceBindingService) CurrentBinding(ctx context.Context, db *gorm.DB, actor, soloID string) (WorkspaceRestoreBinding, error) {
	var parent struct{ SandboxID uint }
	if err := s.options.ReadScope(ctx, db, actor, "solo", soloID); err != nil {
		return WorkspaceRestoreBinding{}, err
	}
	if err := workspaceCurrentRead(db).Table("solo_sessions").Select("sandbox_id").Take(&parent, "id = ? AND user_id = ?", soloID, actor).Error; err != nil {
		return WorkspaceRestoreBinding{}, err
	}
	return s.CurrentSandboxBinding(ctx, db, actor, "solo", soloID, parent.SandboxID)
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

// GuardWorkspaceBindingTx is safe in existing Queue→parent→Sandbox admission
// paths. It does not load artifacts, resolve a root or contact the runtime.
func GuardWorkspaceBindingTx(tx *gorm.DB, sandboxID uint) error {
	if sandboxID == 0 {
		return nil
	}
	var head struct{ State, PendingOperationID string }
	err := workspaceCurrentRead(tx).Table("workspace_binding_heads").Select("state", "pending_operation_id").Take(&head, "sandbox_id = ?", sandboxID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if head.State != "ready" || head.PendingOperationID != "" {
		return soloErr(409, "workspace_switch_pending", "工作区切换尚未确认，请核对原操作后继续")
	}
	return nil
}
