package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"gorm.io/gorm"
)

type workspaceReleaseTestProvider struct {
	mu                        sync.Mutex
	view                      sandbox.EnvironmentReleaseView
	confirms, reads, renewals atomic.Int32
	unsupported, lost, gone   bool
	beforeConfirm             func()
	transientReads            int
}

func (p *workspaceReleaseTestProvider) Capabilities(context.Context, string) (sandbox.EnvironmentReleaseCapabilities, error) {
	return sandbox.EnvironmentReleaseCapabilities{Supported: !p.unsupported, Version: sandbox.EnvironmentReleaseVersion, Reason: "driver_not_supported"}, nil
}
func (p *workspaceReleaseTestProvider) Preview(_ context.Context, resource string, in sandbox.EnvironmentReleasePrepare) (sandbox.EnvironmentReleaseView, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.view = sandbox.EnvironmentReleaseView{ID: in.ID, State: "prepared", Plan: sandbox.EnvironmentReleasePlan{Version: sandbox.EnvironmentReleaseVersion, Resource: resource, Namespace: "owned-test", Objects: []sandbox.EnvironmentReleaseObject{{Kind: "Deployment", Namespace: "owned-test", Name: "deployment", UID: "dep-original"}, {Kind: "PersistentVolumeClaim", Namespace: "owned-test", Name: "volume", UID: "pvc-original"}, {Kind: "PersistentVolume", Name: "physical-volume", UID: "pv-original", ObserveOnly: true}}}}
	p.view.Plan.SHA256 = p.view.Plan.Digest()
	return p.view, nil
}
func (p *workspaceReleaseTestProvider) Confirm(ctx context.Context, _ string, _ string, in sandbox.EnvironmentReleaseConfirm) (sandbox.EnvironmentReleaseView, error) {
	p.confirms.Add(1)
	if p.beforeConfirm != nil {
		p.beforeConfirm()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !in.Confirmed || !in.LossAccepted || in.Generation == 0 || in.Owner == "" || !in.ExpiresAt.After(time.Now()) {
		return sandbox.EnvironmentReleaseView{}, ErrWorkspaceReleaseInvalid
	}
	p.view.State = "observing"
	if p.lost {
		return sandbox.EnvironmentReleaseView{}, errors.New("response lost after accepted dispatch")
	}
	return p.observed(), nil
}
func (p *workspaceReleaseTestProvider) Renew(_ context.Context, _ string, _ string, _ sandbox.EnvironmentReleaseRenew) (sandbox.EnvironmentReleaseView, error) {
	p.renewals.Add(1)
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.observed(), nil
}
func (p *workspaceReleaseTestProvider) observed() sandbox.EnvironmentReleaseView {
	out := p.view
	if p.gone {
		out.State = "released"
		out.Receipt = &sandbox.EnvironmentReleaseReceipt{ID: out.ID, Resource: out.Plan.Resource, Lineage: "test-release-lineage", PlanSHA256: out.Plan.SHA256, Objects: out.Plan.Objects, VerifiedAt: time.Now().UTC()}
	}
	return out
}
func (p *workspaceReleaseTestProvider) Get(context.Context, string, string) (sandbox.EnvironmentReleaseView, error) {
	p.reads.Add(1)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.transientReads > 0 {
		p.transientReads--
		return sandbox.EnvironmentReleaseView{}, errors.New("temporary transport outage")
	}
	return p.observed(), nil
}

type workspaceReleaseTestPrepared struct{}

func workspaceReleaseFixture(t *testing.T) (*WorkspaceReleaseService, *workspaceReleaseTestProvider, *atomic.Bool, models.SoloSession) {
	t.Helper()
	solo, sbx, _, db := newSoloFixture(t, 0, 2)
	if err := db.AutoMigrate(&models.WorkspaceRelease{}, &models.WorkspaceBindingHead{}, &models.WorkspaceBinding{}, &models.WorkspaceReturnPoint{}, &models.WorkspaceBindingOperation{}, &models.WorkspaceBindingUse{}); err != nil {
		t.Fatal(err)
	}
	source := models.Sandbox{Name: "native-release-environment", Purpose: SandboxPurposeSolo, ThreadID: "release-solo", Status: "stopped", DesiredState: "stopped"}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	parent := models.SoloSession{ID: source.ThreadID, UserID: "alice", AgentName: "agentA", SandboxID: source.ID, Status: models.SoloStatusClosed, LifecycleEpoch: 8, LifecycleAttemptID: "original-attempt"}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatal(err)
	}
	provider := &workspaceReleaseTestProvider{gone: true}
	revoked := &atomic.Bool{}
	service, err := NewWorkspaceReleaseService(db, solo, sbx, WorkspaceReleaseOptions{Lineage: "test-release-lineage", Provider: provider, Bindings: &WorkspaceBindingService{db: db}, PollInterval: 5 * time.Millisecond, Prepare: func(ctx context.Context) (context.Context, error) {
		if revoked.Load() {
			return ctx, ErrWorkspaceJobAuthority
		}
		return context.WithValue(ctx, workspaceReleaseTestPrepared{}, true), nil
	}, GuardTx: func(ctx context.Context, tx *gorm.DB, _ models.WorkspaceRelease) error {
		if ctx.Value(workspaceReleaseTestPrepared{}) != true || revoked.Load() {
			return ErrWorkspaceJobAuthority
		}
		if _, inside := tx.Statement.ConnPool.(gorm.TxCommitter); !inside {
			return ErrWorkspaceReleaseInvalid
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	// Production installs this startup admission hook. Tests exercise that same
	// real guard; metadata-only fixtures do not manufacture a bypass capability.
	sbx.workspaceReleaseGuard = GuardWorkspaceReleaseTx
	return service, provider, revoked, parent
}
func workspaceReleasePrepared(t *testing.T, s *WorkspaceReleaseService) WorkspaceReleaseView {
	t.Helper()
	v, e := s.Preview(t.Context(), "alice", "release-solo", WorkspaceReleasePreviewInput{ClientRequestID: "preview-original"})
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func workspaceReleaseConfirmed(t *testing.T, s *WorkspaceReleaseService) WorkspaceReleaseView {
	t.Helper()
	v := workspaceReleasePrepared(t, s)
	v, e := s.Confirm(t.Context(), "alice", "release-solo", v.ID, WorkspaceReleaseConfirmInput{ClientRequestID: "confirm-original", Confirmed: true, LossAccepted: true})
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestWorkspaceReleaseExplicitLossScopeKeepsChatSnapshotsAndReturns(t *testing.T) {
	s, p, _, parent := workspaceReleaseFixture(t)
	now := time.Now().UTC()
	snapshot := models.WorkspaceSnapshot{ID: "saved-snapshot", ActorID: "alice", ScopeKind: "solo", ScopeID: parent.ID, SourceSandboxID: parent.SandboxID, State: "ready", VerifiedAt: &now, ArchiveSHA256: strings.Repeat("a", 64), ManifestSHA256: strings.Repeat("b", 64)}
	if err := s.db.Create(&snapshot).Error; err != nil {
		t.Fatal(err)
	}
	point := models.WorkspaceReturnPoint{ID: "independent-return", ActorID: "alice", SoloID: parent.ID, SandboxID: parent.SandboxID, SnapshotID: snapshot.ID, State: "ready"}
	if err := s.db.Create(&point).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.db.Create(&models.SoloMessage{ID: "original-message", SessionID: parent.ID, Role: "user", Content: "preserve original chat"}).Error; err != nil {
		t.Fatal(err)
	}
	preview := workspaceReleasePrepared(t, s)
	if !preview.WholeEnvironment || !preview.LossConfirmationRequired || preview.SnapshotsRetained != 1 || preview.ReturnPointsRetained != 1 || p.confirms.Load() != 0 {
		t.Fatal(preview)
	}
	if _, err := s.Get(t.Context(), "mallory", parent.ID, preview.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("other owner read release", err)
	}
	if _, err := s.Confirm(t.Context(), "alice", parent.ID, preview.ID, WorkspaceReleaseConfirmInput{ClientRequestID: "confirm-original", Confirmed: true}); !errors.Is(err, ErrWorkspaceReleaseInvalid) {
		t.Fatal("loss acceptance optional", err)
	}
	accepted := workspaceReleaseConfirmed(t, s)
	if accepted.State != "confirmed" || p.confirms.Load() != 0 {
		t.Fatal("browser request dispatched native operation")
	}
	if _, _, err := s.sbx.acquireSandboxOperation(t.Context(), parent.SandboxID, "unrelated-operation"); !errors.Is(err, ErrWorkspaceReleaseBlocked) {
		t.Fatal("new operation crossed release barrier", err)
	}
	// The provider is outside SQL transactions; acquiring a fresh metadata
	// transaction here would deadlock if confirmation retained a publication lock.
	p.beforeConfirm = func() {
		if err := s.db.Transaction(func(tx *gorm.DB) error {
			return tx.Create(&models.Setting{Key: "release-native-outside-tx", Value: "yes"}).Error
		}); err != nil {
			t.Error(err)
		}
	}
	if err := s.RunOne(t.Context(), accepted.ID); err != nil {
		t.Fatal(err)
	}
	view, err := s.Get(t.Context(), "alice", parent.ID, accepted.ID)
	if err != nil || view.State != "released" {
		t.Fatal(view, err)
	}
	var kept models.SoloSession
	if err := s.db.Take(&kept, "id = ?", parent.ID).Error; err != nil {
		t.Fatal(err)
	}
	if kept.SandboxID != 0 || kept.Status != models.SoloStatusClosed || kept.LifecycleAction != "" || !kept.ConversationContextPending {
		t.Fatal(kept)
	}
	var sb models.Sandbox
	_ = s.db.Take(&sb, parent.SandboxID).Error
	if sb.Status != "released" || sb.Name != "native-release-environment" {
		t.Fatal(sb)
	}
	var count int64
	s.db.Model(&models.WorkspaceSnapshot{}).Where("id = ? AND state = 'ready'", snapshot.ID).Count(&count)
	if count != 1 {
		t.Fatal("snapshot pin removed")
	}
	s.db.Model(&models.WorkspaceReturnPoint{}).Where("id = ?", point.ID).Count(&count)
	if count != 1 {
		t.Fatal("return metadata removed")
	}
	s.db.Model(&models.SoloMessage{}).Where("session_id = ?", parent.ID).Count(&count)
	if count != 2 {
		t.Fatal("chat/result publication incorrect", count)
	}
	if _, err := s.Confirm(t.Context(), "alice", parent.ID, accepted.ID, WorkspaceReleaseConfirmInput{ClientRequestID: "confirm-original", Confirmed: true, LossAccepted: true}); err != nil {
		t.Fatal("original receipt unavailable", err)
	}
	if err := s.RunOne(t.Context(), accepted.ID); !errors.Is(err, ErrWorkspaceReleaseUnknown) {
		t.Fatal("replayed released operation", err)
	}
	if p.confirms.Load() != 1 {
		t.Fatal("more than one physical dispatch")
	}
}
func TestWorkspaceReleaseUnknownRestartOnlyChecksOriginalReceipt(t *testing.T) {
	s, p, revoked, parent := workspaceReleaseFixture(t)
	p.lost = true
	p.gone = false
	view := workspaceReleaseConfirmed(t, s)
	if err := s.RunOne(t.Context(), view.ID); !errors.Is(err, ErrWorkspaceReleaseUnknown) {
		t.Fatal(err)
	}
	restart, err := NewWorkspaceReleaseService(s.db, s.solo, s.sbx, s.options)
	if err != nil {
		t.Fatal(err)
	}
	if err := restart.RunOne(t.Context(), view.ID); !errors.Is(err, ErrWorkspaceReleaseUnknown) {
		t.Fatal("restart repeated dispatch", err)
	}
	for range 2 {
		got, err := restart.Reconcile(t.Context(), "alice", parent.ID, view.ID)
		if err != nil || got.State != "unknown" {
			t.Fatal(got, err)
		}
	}
	if p.confirms.Load() != 1 || p.renewals.Load() != 0 {
		t.Fatal("reconcile mutated provider")
	}
	p.mu.Lock()
	p.gone = true
	p.mu.Unlock()
	revoked.Store(true)
	if _, err := restart.Reconcile(t.Context(), "alice", parent.ID, view.ID); err == nil {
		t.Fatal("revoked authority published release")
	}
	kept, _ := s.solo.Require(parent.ID, "alice")
	if kept.SandboxID != parent.SandboxID {
		t.Fatal("unknown result cleared binding")
	}
	if _, err := restart.Get(t.Context(), "alice", parent.ID, view.ID); err != nil {
		t.Fatal("historical receipt required execution authority", err)
	}
	revoked.Store(false)
	got, err := restart.Reconcile(t.Context(), "alice", parent.ID, view.ID)
	if err != nil || got.State != "released" {
		t.Fatal(got, err)
	}
	if p.confirms.Load() != 1 || p.renewals.Load() != 0 {
		t.Fatal("reconcile repeated physical mutation")
	}
}
func TestWorkspaceReleaseRejectsStaleScopeAndKeepsOriginalEnvironment(t *testing.T) {
	for _, change := range []string{"binding", "queue", "job", "epoch", "authority", "unsupported"} {
		t.Run(change, func(t *testing.T) {
			s, p, revoked, parent := workspaceReleaseFixture(t)
			if change == "unsupported" {
				p.unsupported = true
				if _, err := s.Preview(t.Context(), "alice", parent.ID, WorkspaceReleasePreviewInput{ClientRequestID: "preview-original"}); !errors.Is(err, ErrWorkspaceReleaseUnsupported) {
					t.Fatal(err)
				}
				return
			}
			view := workspaceReleasePrepared(t, s)
			switch change {
			case "binding":
				if err := s.db.Create(&models.WorkspaceBinding{ID: "new-root", SoloID: parent.ID, SandboxID: parent.SandboxID, State: "retained"}).Error; err != nil {
					t.Fatal(err)
				}
			case "queue":
				if err := s.db.Create(&models.ChatQueueTurn{ID: "queued-late", QueueID: soloQueueScope(parent.ID).ID(), State: "queued"}).Error; err != nil {
					t.Fatal(err)
				}
			case "job":
				if err := s.db.Create(&models.WorkspaceSnapshotJob{ID: "unknown-snapshot-job", SandboxID: parent.SandboxID, State: "unknown"}).Error; err != nil {
					t.Fatal(err)
				}
			case "epoch":
				s.db.Model(&models.SoloSession{}).Where("id = ?", parent.ID).Update("lifecycle_epoch", 9)
			case "authority":
				revoked.Store(true)
			}
			_, err := s.Confirm(t.Context(), "alice", parent.ID, view.ID, WorkspaceReleaseConfirmInput{ClientRequestID: "confirm-original", Confirmed: true, LossAccepted: true})
			if err == nil {
				t.Fatal("changed scope accepted")
			}
			if p.confirms.Load() != 0 {
				t.Fatal("failed confirmation dispatched")
			}
			kept, _ := s.solo.Require(parent.ID, "alice")
			if kept.SandboxID != parent.SandboxID {
				t.Fatal("failed confirmation cleared original")
			}
		})
	}
}
func TestWorkspaceReleaseTransientObservationRetriesWithinLease(t *testing.T) {
	s, p, _, _ := workspaceReleaseFixture(t)
	p.gone = false
	p.transientReads = 2
	view := workspaceReleaseConfirmed(t, s)
	done := make(chan error, 1)
	go func() { done <- s.RunOne(t.Context(), view.ID) }()
	deadline := time.Now().Add(time.Second)
	for p.reads.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if p.reads.Load() < 3 {
		t.Fatal("temporary read failure ended durable operation")
	}
	p.mu.Lock()
	p.gone = true
	p.mu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("release never completed")
	}
	if p.confirms.Load() != 1 {
		t.Fatal("transient reads caused redispatch")
	}
}
func TestWorkspaceReleaseFinalScopeFenceRejectsLateMutation(t *testing.T) {
	s, p, _, parent := workspaceReleaseFixture(t)
	view := workspaceReleaseConfirmed(t, s)
	p.beforeConfirm = func() {
		if err := s.db.Create(&models.WorkspaceBinding{ID: "late-root", SoloID: parent.ID, SandboxID: parent.SandboxID, State: "retained"}).Error; err != nil {
			t.Error(err)
		}
	}
	if err := s.RunOne(t.Context(), view.ID); !errors.Is(err, ErrWorkspaceReleaseConflict) {
		t.Fatal("final source mutation accepted", err)
	}
	kept, _ := s.solo.Require(parent.ID, "alice")
	if kept.SandboxID != parent.SandboxID {
		t.Fatal("late scope change cleared binding")
	}
	var result models.WorkspaceRelease
	_ = s.db.Take(&result, "id = ?", view.ID).Error
	if result.State == "released" {
		t.Fatal("late publication ignored scope fence")
	}
}
