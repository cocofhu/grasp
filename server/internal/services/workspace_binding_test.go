package services

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

type bindingTestRemote struct {
	caps   sandbox.WorkspaceBindingCapabilities
	view   sandbox.WorkspaceBindingView
	calls  atomic.Int32
	before func()
}

func (r *bindingTestRemote) WorkspaceBindingCapabilities(context.Context) (sandbox.WorkspaceBindingCapabilities, error) {
	r.calls.Add(1)
	if r.before != nil {
		r.before()
	}
	return r.caps, nil
}
func (r *bindingTestRemote) WorkspaceBindingHead(context.Context) (sandbox.WorkspaceBindingView, error) {
	r.calls.Add(1)
	return r.view, nil
}
func (r *bindingTestRemote) WorkspaceBindingOperation(context.Context, string) (sandbox.WorkspaceBindingOperation, error) {
	return sandbox.WorkspaceBindingOperation{}, gorm.ErrRecordNotFound
}

type bindingTestPrepare struct{}

func nativeBindingFingerprint(head sandbox.WorkspaceBindingHead) string {
	head.Reference.Fingerprint = ""
	return bindingDigest(head)
}

func bindingResolverFixture(t *testing.T) (*WorkspaceBindingService, *bindingTestRemote, models.SoloSession, *atomic.Bool) {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.WorkspaceBindingHead{}, &models.WorkspaceBinding{}, &models.WorkspaceReturnPoint{}, &models.WorkspaceBindingOperation{}, &models.WorkspaceBindingUse{}); err != nil {
		t.Fatal(err)
	}
	sbx := &SandboxService{db: db, live: map[uint]*liveSandbox{}}
	row := models.Sandbox{Name: "canonical-resource", Purpose: SandboxPurposeSolo, ThreadID: "canonical-solo", Status: "running", DesiredState: "running", Profile: "agent-original", Generation: 17, AuthorizationFingerprint: strings.Repeat("c", 64)}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	solo := models.SoloSession{ID: row.ThreadID, UserID: "alice", SandboxID: row.ID, Status: models.SoloStatusRunning, LifecycleEpoch: 3, LifecycleAttemptID: "current-attempt"}
	if err := db.Create(&solo).Error; err != nil {
		t.Fatal(err)
	}
	head := sandbox.WorkspaceBindingHead{Version: 1, Resource: row.Name, Lineage: "lineage-original", Reference: sandbox.WorkspaceBindingReference{ID: "original-binding", Revision: 1}, Target: sandbox.WorkspaceBindingSelector{Kind: "original"}}
	head.Reference.Fingerprint = nativeBindingFingerprint(head)
	remote := &bindingTestRemote{caps: sandbox.WorkspaceBindingCapabilities{Version: 1, Resource: row.Name, Lineage: head.Lineage, Activate: true, Return: true, DurableHead: true, PortOwnership: true, Consumers: []string{"agent", "capture", "code"}}, view: sandbox.WorkspaceBindingView{Head: head, Path: t.TempDir(), Ready: true}}
	revoked := &atomic.Bool{}
	options := WorkspaceBindingOptions{ExpectedLineage: head.Lineage, Prepare: func(ctx context.Context) (context.Context, error) {
		return context.WithValue(ctx, bindingTestPrepare{}, true), nil
	}, ReadScope: func(ctx context.Context, db *gorm.DB, actor, kind, id string) error {
		if kind != "solo" || id != solo.ID {
			return gorm.ErrRecordNotFound
		}
		var n int64
		err := db.WithContext(ctx).Table("solo_sessions").Where("id = ? AND user_id = ?", id, actor).Count(&n).Error
		if err != nil {
			return err
		}
		if n != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	}, CurrentAuthorization: func(context.Context, string, string, string, uint) (string, error) {
		if revoked.Load() {
			return "", ErrWorkspaceJobAuthority
		}
		return strings.Repeat("e", 64), nil
	}, GuardTx: func(ctx context.Context, tx *gorm.DB, _ string, _ string, _ uint) error {
		if value, _ := ctx.Value(bindingTestPrepare{}).(bool); !value {
			return ErrWorkspaceJobAuthority
		}
		if _, ok := tx.Statement.ConnPool.(gorm.TxCommitter); !ok {
			return ErrWorkspaceJobInvalid
		}
		return nil
	}, Remote: func(context.Context, uint) (WorkspaceBindingRemote, error) { return remote, nil }}
	service, err := NewWorkspaceBindingService(db, sbx, options)
	if err != nil {
		t.Fatal(err)
	}
	return service, remote, solo, revoked
}
func TestWorkspaceBindingResolvePinsLogicalRootAndMetadataReadsNeverContactRuntime(t *testing.T) {
	s, remote, solo, _ := bindingResolverFixture(t)
	remote.before = func() {
		if err := s.db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&models.Sandbox{}).Where("id = ?", solo.SandboxID).Update("generation", 18).Error
		}); err != nil {
			t.Fatal("native I/O retained an SQL lock", err)
		}
	}
	value, err := s.Resolve(t.Context(), "alice", solo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if value.Fingerprint == "" || value.Reference != remote.view.Head.Reference || value.Path != remote.view.Path {
		t.Fatal(value)
	}
	before := remote.calls.Load()
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		got, err := s.CurrentBinding(t.Context(), tx, "alice", solo.ID)
		if err != nil {
			return err
		}
		if got.Fingerprint != value.Fingerprint || got.SandboxID != value.SandboxID {
			t.Fatal(got, value)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if remote.calls.Load() != before {
		t.Fatal("metadata resolver performed native I/O")
	}
	if err := s.db.Model(&models.Sandbox{}).Where("id = ?", solo.SandboxID).Update("generation", 900).Error; err != nil {
		t.Fatal(err)
	}
	got, err := s.CurrentBinding(t.Context(), s.db, "alice", solo.ID)
	if err != nil || got.Fingerprint != value.Fingerprint {
		t.Fatal("physical generation changed logical identity", got, err)
	}
}
func TestWorkspaceBindingResolveRejectsWrongOwnerRevokedGrantAndDifferentRuntimeLineage(t *testing.T) {
	for _, kind := range []string{"owner", "grant", "lineage", "resource", "native_hash"} {
		t.Run(kind, func(t *testing.T) {
			s, remote, solo, revoked := bindingResolverFixture(t)
			actor := "alice"
			switch kind {
			case "owner":
				actor = "bob"
			case "grant":
				revoked.Store(true)
			case "lineage":
				remote.caps.Lineage = "different-valid-lineage"
			case "resource":
				remote.caps.Resource = "different-resource"
			case "native_hash":
				remote.view.Head.Reference.Fingerprint = strings.Repeat("a", 64)
			}
			if _, err := s.Resolve(t.Context(), actor, solo.ID); err == nil {
				t.Fatal("invalid current binding was published")
			}
			var n int64
			if err := s.db.Model(&models.WorkspaceBindingHead{}).Count(&n).Error; err != nil || n != 0 {
				t.Fatal("rejected runtime created a canonical head", n, err)
			}
		})
	}
}
func TestWorkspaceBindingResolveCannotAdoptChangedNativeHeadOrPendingSwitch(t *testing.T) {
	s, remote, solo, _ := bindingResolverFixture(t)
	value, err := s.Resolve(t.Context(), "alice", solo.ID)
	if err != nil {
		t.Fatal(err)
	}
	remote.view.Head.Reference.ID = "unexpected-new-binding"
	remote.view.Head.Reference.Revision++
	remote.view.Head.ParentID = value.Reference.ID
	remote.view.Head.Target = sandbox.WorkspaceBindingSelector{Kind: "restore", ID: "restored-directory"}
	remote.view.Head.Reference.Fingerprint = nativeBindingFingerprint(remote.view.Head)
	if _, err := s.Resolve(t.Context(), "alice", solo.ID); !errors.Is(err, ErrWorkspaceJobUnknown) {
		t.Fatal("read adopted an unconfirmed side effect", err)
	}
	if err := s.db.Model(&models.WorkspaceBindingHead{}).Where("sandbox_id = ?", solo.SandboxID).Updates(map[string]any{"state": "pending", "pending_operation_id": "original-pending-operation"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.CurrentBinding(t.Context(), s.db, "alice", solo.ID); !errors.Is(err, ErrWorkspaceJobConflict) {
		t.Fatal("metadata bypassed switch barrier", err)
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error { return GuardWorkspaceBindingTx(tx, solo.SandboxID) }); err == nil {
		t.Fatal("new turn admitted through unknown switch")
	}
}
func TestWorkspaceBindingReleaseFingerprintIncludesRealReturnAndRawSnapshotIdentity(t *testing.T) {
	s, _, solo, _ := bindingResolverFixture(t)
	if _, err := s.Resolve(t.Context(), "alice", solo.ID); err != nil {
		t.Fatal(err)
	}
	point := models.WorkspaceReturnPoint{ID: "return-original", ActorID: "alice", SoloID: solo.ID, SandboxID: solo.SandboxID, SnapshotID: "snapshot-original", State: "ready"}
	if err := s.db.Create(&point).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Table("workspace_snapshots").Create(map[string]any{"id": point.SnapshotID, "state": "ready", "archive_ref": "original-raw-ref", "source_binding_fingerprint": "source-binding"}).Error; err != nil {
		t.Fatal(err)
	}
	read := func() WorkspaceReleaseBindingScope {
		var scope WorkspaceReleaseBindingScope
		if err := s.db.Transaction(func(tx *gorm.DB) error {
			var err error
			scope, err = s.ReleaseScope(t.Context(), tx, solo.ID, solo.SandboxID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return scope
	}
	first := read()
	if first.ReturnPoints != 1 || first.Bindings != 1 {
		t.Fatal(first)
	}
	if err := s.db.Table("workspace_snapshots").Where("id = ?", point.SnapshotID).Update("archive_ref", "changed-raw-ref").Error; err != nil {
		t.Fatal(err)
	}
	second := read()
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("raw snapshot replacement did not change loss scope")
	}
	use := models.WorkspaceBindingUse{ID: "active-terminal", SandboxID: solo.SandboxID, Kind: "terminal", State: "active"}
	if err := s.db.Create(&use).Error; err != nil {
		t.Fatal(err)
	}
	blocked := read()
	if len(blocked.Blockers) != 1 || blocked.Blockers[0] != "workspace_consumers_busy" {
		t.Fatal(blocked)
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error { return s.EnvironmentStoppedTx(t.Context(), tx, solo.SandboxID) }); err != nil {
		t.Fatal(err)
	}
	after := read()
	if len(after.Blockers) != 0 {
		t.Fatal(after)
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		return s.ReleasedTx(t.Context(), tx, models.WorkspaceRelease{SoloID: solo.ID, SandboxID: solo.SandboxID, BindingsFingerprint: after.Fingerprint})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CurrentBinding(t.Context(), s.db, "alice", solo.ID); err == nil {
		t.Fatal("released disk remained active")
	}
	var count int64
	if err := s.db.Model(&models.WorkspaceReturnPoint{}).Where("id = ?", point.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("environment release erased independent Return", count, err)
	}
}

func TestWorkspaceBindingReleaseLegacyEnvironmentWithoutHeadStillHasStableWholeDiskScope(t *testing.T) {
	s, _, solo, _ := bindingResolverFixture(t)
	var first WorkspaceReleaseBindingScope
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		first, err = s.ReleaseScope(t.Context(), tx, solo.ID, solo.SandboxID)
		return err
	}); err != nil {
		t.Fatal("legacy missing head poisoned following GORM queries", err)
	}
	if first.Bindings != 1 || first.ReturnPoints != 0 || len(first.Blockers) != 0 || !validDigest(first.Fingerprint) {
		t.Fatal(first)
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		again, err := s.ReleaseScope(t.Context(), tx, solo.ID, solo.SandboxID)
		if err == nil && again.Fingerprint != first.Fingerprint {
			t.Fatal("unstable legacy scope", again, first)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceBindingDisabledNewActionsStillReadCanonicalHeadAndUnavailableResolverNeverFallsBack(t *testing.T) {
	s, _, solo := bindingAccessFixture(t)
	s.options.FileOperationsEnabled = false
	view, err := s.CurrentView(t.Context(), "alice", solo.ID)
	if err != nil || !view.Available || view.CanCapture || view.CanRestore || view.CanActivate || view.CanReturn {
		t.Fatal(view, err)
	}
	s.sandbox.SetWorkspaceBindings(nil)
	if _, _, release, err := s.sandbox.beginSandboxAccess(t.Context(), solo.SandboxID); err == nil {
		release()
		t.Fatal("disabled resolver fell back to original cwd for a durable canonical head")
	}
}
