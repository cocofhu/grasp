package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/database"
	"sandbox-gateway/internal/driver"
	driverfake "sandbox-gateway/internal/driver/fake"
	"sandbox-gateway/internal/models"
	"sandbox-gateway/internal/store"
)

type releaseNativeFixture struct {
	*driverfake.Driver
	mu          sync.Mutex
	plan        driver.EnvironmentReleasePlan
	gone        bool
	deleteError error
	checkError  error
	onDelete    func(context.Context, func(context.Context) error) error
	deletes     atomic.Int32
}

func (f *releaseNativeFixture) PreviewEnvironmentRelease(context.Context, string) (driver.EnvironmentReleasePlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.plan, f.checkError
}
func (f *releaseNativeFixture) CheckEnvironmentRelease(context.Context, driver.EnvironmentReleasePlan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.checkError
}
func (f *releaseNativeFixture) DeleteEnvironmentRelease(ctx context.Context, _ driver.EnvironmentReleasePlan, guard func(context.Context) error) error {
	if err := guard(ctx); err != nil {
		return err
	}
	f.deletes.Add(1)
	if f.onDelete != nil {
		return f.onDelete(ctx, guard)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteError != nil {
		return f.deleteError
	}
	f.gone = true
	return nil
}
func (f *releaseNativeFixture) EnvironmentReleaseGone(context.Context, driver.EnvironmentReleasePlan) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gone, nil
}
func releaseServiceFixture(t *testing.T, options EnvironmentReleaseOptions) (*EnvironmentReleases, *releaseNativeFixture, *gorm.DB) {
	t.Helper()
	db, err := database.Open(config.DBConfig{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "release.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	native := &releaseNativeFixture{Driver: driverfake.New(), plan: driver.EnvironmentReleasePlan{Version: driver.EnvironmentReleaseVersion, Resource: "environment-one", Namespace: "sandboxes", Objects: []driver.EnvironmentReleaseObject{{Kind: "Deployment", Namespace: "sandboxes", Name: "sbx-environment-one", UID: "deployment-original"}, {Kind: "PersistentVolumeClaim", Namespace: "sandboxes", Name: "sbx-environment-one-data", UID: "pvc-original"}, {Kind: "PersistentVolume", Name: "pv-original", UID: "volume-original", ObserveOnly: true}}}}
	native.plan.SHA256 = native.plan.Digest()
	sb := models.Sandbox{ID: "environment-one", Name: "environment-one", Status: models.StatusStopped}
	sb.SetEnv(map[string]string{"GRASP_EXECUTION_LINEAGE": "lineage-one"})
	if err := db.Create(&sb).Error; err != nil {
		t.Fatal(err)
	}
	svc := New(native, store.New(db), Config{})
	return NewEnvironmentReleases(db, svc, options), native, db
}
func confirmedRelease(t *testing.T, r *EnvironmentReleases, id string) EnvironmentReleaseView {
	t.Helper()
	view, err := r.Preview(context.Background(), "environment-one", EnvironmentReleasePrepare{ID: id, Lineage: "lineage-one"})
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().UTC().Add(r.options.LeaseTTL - 5*time.Millisecond)
	view, err = r.Confirm(context.Background(), "environment-one", id, EnvironmentReleaseConfirm{PlanSHA256: view.Plan.SHA256, Nonce: "manual-confirm-original", Confirmed: true, LossAccepted: true, Owner: "owner-original", Generation: 1, ExpiresAt: until, Deadline: time.Now().UTC().Add(r.options.MaxDuration - time.Millisecond)})
	if err != nil {
		t.Fatal(err)
	}
	return view
}
func TestEnvironmentReleaseDurableConfirmationAndOriginalReceipt(t *testing.T) {
	r, native, db := releaseServiceFixture(t, EnvironmentReleaseOptions{})
	ctx := context.Background()
	preview, err := r.Preview(ctx, "environment-one", EnvironmentReleasePrepare{ID: "original-release", Lineage: "lineage-one"})
	if err != nil {
		t.Fatal(err)
	}
	invalid := EnvironmentReleaseConfirm{PlanSHA256: preview.Plan.SHA256, Nonce: "manual-confirm-original", Confirmed: true, Owner: "owner-original", Generation: 1, ExpiresAt: time.Now().Add(time.Second), Deadline: time.Now().Add(time.Minute)}
	if _, err := r.Confirm(ctx, "environment-one", preview.ID, invalid); !errors.Is(err, ErrReleaseInvalid) {
		t.Fatal("loss confirmation was optional", err)
	}
	if native.deletes.Load() != 0 {
		t.Fatal("preview invoked deletion")
	}
	view := confirmedRelease(t, r, preview.ID)
	if view.State != "confirmed" || native.deletes.Load() != 0 {
		t.Fatal("HTTP confirmation executed a native job", view.State)
	}
	if err := r.sandbox.Start(ctx, "environment-one"); !errors.Is(err, driver.ErrEnvironmentReleasePending) {
		t.Fatalf("start crossed pending release: %v", err)
	}
	if err := r.sandbox.Reinstall(ctx, "environment-one", true); !errors.Is(err, driver.ErrEnvironmentReleasePending) {
		t.Fatalf("reinstall crossed pending release: %v", err)
	}
	if err := r.sandbox.Destroy(ctx, "environment-one"); !errors.Is(err, driver.ErrEnvironmentReleasePending) {
		t.Fatalf("legacy destroy crossed pending release: %v", err)
	}
	if err := r.RunOne(ctx, preview.ID); err != nil {
		t.Fatal(err)
	}
	view, err = r.Get(ctx, "environment-one", preview.ID)
	if err != nil || view.State != "released" || view.Receipt == nil || native.deletes.Load() != 1 {
		t.Fatalf("missing physical receipt: %+v %v", view, err)
	}
	other := NewEnvironmentReleases(db, r.sandbox, EnvironmentReleaseOptions{})
	if err := other.RunOne(ctx, preview.ID); !errors.Is(err, ErrReleaseConflict) {
		t.Fatalf("replayed native operation: %v", err)
	}
	view, err = other.Get(ctx, "environment-one", preview.ID)
	if err != nil || view.Receipt.PlanSHA256 != preview.Plan.SHA256 {
		t.Fatal("restart lost original receipt", err)
	}
	var sb models.Sandbox
	_ = db.Take(&sb, "id = ?", "environment-one").Error
	if sb.Status != "released" {
		t.Fatal("control tombstone not retained")
	}
}
func TestEnvironmentReleaseUnknownOnlyReadsAndNeverReplays(t *testing.T) {
	r, native, db := releaseServiceFixture(t, EnvironmentReleaseOptions{})
	ctx := context.Background()
	native.deleteError = errors.New("ack lost after partial native deletion")
	view := confirmedRelease(t, r, "uncertain-release")
	if err := r.RunOne(ctx, view.ID); err == nil {
		t.Fatal("native failure was hidden")
	}
	restart := NewEnvironmentReleases(db, r.sandbox, EnvironmentReleaseOptions{})
	for range 3 {
		view, err := restart.Get(ctx, "environment-one", view.ID)
		if err != nil || view.State != "unknown" {
			t.Fatalf("unknown was silently successful: %s %v", view.State, err)
		}
	}
	if native.deletes.Load() != 1 {
		t.Fatal("read-only checks repeated deletion")
	}
	native.mu.Lock()
	native.gone = true
	native.mu.Unlock()
	view, err := restart.Get(ctx, "environment-one", view.ID)
	if err != nil || view.State != "released" || view.Receipt == nil {
		t.Fatalf("read-only physical proof was not persisted: %+v %v", view, err)
	}
	if native.deletes.Load() != 1 {
		t.Fatal("reconciliation repeated mutation")
	}
}
func TestEnvironmentReleaseCrashMarkerAndExpiryAreNotReplayPermission(t *testing.T) {
	r, native, db := releaseServiceFixture(t, EnvironmentReleaseOptions{LeaseTTL: 80 * time.Millisecond, PollInterval: 10 * time.Millisecond})
	view := confirmedRelease(t, r, "crashed-release")
	now := time.Now().UTC()
	if err := db.Model(&models.EnvironmentRelease{}).Where("id = ?", view.ID).Updates(map[string]any{"state": "dispatching", "dispatched_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	restarted := NewEnvironmentReleases(db, r.sandbox, r.options)
	ctx, cancel := context.WithTimeout(context.Background(), 130*time.Millisecond)
	defer cancel()
	restarted.Run(ctx)
	var saved models.EnvironmentRelease
	_ = db.Take(&saved, "id = ?", view.ID).Error
	if saved.State != "unknown" || native.deletes.Load() != 0 {
		t.Fatalf("crash replayed native operation: %s %d", saved.State, native.deletes.Load())
	}
	if _, err := restarted.Renew(context.Background(), "environment-one", view.ID, EnvironmentReleaseRenew{Owner: "owner-original", Generation: 1, ExpiresAt: time.Now().Add(50 * time.Millisecond)}); !errors.Is(err, ErrReleaseOwner) {
		t.Fatalf("expired owner revived: %v", err)
	}
}
func TestEnvironmentReleaseLongOperationRenewsWithoutExtendingOverallDeadline(t *testing.T) {
	r, native, db := releaseServiceFixture(t, EnvironmentReleaseOptions{LeaseTTL: 2 * time.Second, MaxDuration: 6 * time.Second})
	view := confirmedRelease(t, r, "long-release")
	var original models.EnvironmentRelease
	if err := db.Take(&original, "id = ?", view.ID).Error; err != nil || original.ExpiresAt == nil {
		t.Fatal("initial lease was not persisted", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	started := make(chan struct{})
	native.onDelete = func(work context.Context, guard func(context.Context) error) error {
		close(started)
		// Keep native work blocked beyond the original lease, with enough
		// renewal margin for the race detector and concurrent CI packages.
		timer := time.NewTimer(2500 * time.Millisecond)
		defer timer.Stop()
		ticker := time.NewTicker(40 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-work.Done():
				return work.Err()
			case <-ticker.C:
				if err := guard(work); err != nil {
					return err
				}
			case <-timer.C:
				native.mu.Lock()
				native.gone = true
				native.mu.Unlock()
				return nil
			}
		}
	}
	done := make(chan error, 1)
	go func() { done <- r.RunOne(ctx, view.ID) }()
	select {
	case <-started:
	case err := <-done:
		t.Fatal("native work did not start", err)
	case <-ctx.Done():
		t.Fatal("native work did not start", ctx.Err())
	}
	renewals := 0
	loop := time.NewTicker(100 * time.Millisecond)
	defer loop.Stop()
	for {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			if renewals < 5 {
				t.Fatalf("long task skipped lease renewals: %d", renewals)
			}
			var row models.EnvironmentRelease
			if err := db.Take(&row, "id = ?", view.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !row.Deadline.Equal(view.Deadline) {
				t.Fatal("renewal changed original deadline")
			}
			if !time.Now().After(*original.ExpiresAt) || row.ExpiresAt == nil || !row.ExpiresAt.After(*original.ExpiresAt) {
				t.Fatal("native work did not cross the original lease expiry with a renewed lease")
			}
			if row.State != "released" || native.deletes.Load() != 1 {
				t.Fatal("long operation lost its original release receipt", row.State, native.deletes.Load())
			}
			return
		case <-loop.C:
			renewed, err := r.Renew(ctx, "environment-one", view.ID, EnvironmentReleaseRenew{Owner: "owner-original", Generation: 1, ExpiresAt: time.Now().UTC().Add(1900 * time.Millisecond)})
			if errors.Is(err, ErrReleaseOwner) {
				var row models.EnvironmentRelease
				_ = db.Take(&row, "id = ?", view.ID).Error
				if row.State == "released" {
					continue
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if !renewed.Deadline.Equal(view.Deadline) {
				t.Fatal("renewal response changed original deadline")
			}
			renewals++
		case <-ctx.Done():
			t.Fatal("long operation did not finish", ctx.Err())
		}
	}
}

func TestEnvironmentReleaseNewGenerationCancelsBlockedNativeOwner(t *testing.T) {
	r, native, db := releaseServiceFixture(t, EnvironmentReleaseOptions{LeaseTTL: 300 * time.Millisecond})
	view := confirmedRelease(t, r, "generation-lost-release")
	started := make(chan struct{})
	stopped := make(chan struct{})
	native.onDelete = func(ctx context.Context, _ func(context.Context) error) error {
		close(started)
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}
	done := make(chan error, 1)
	go func() { done <- r.RunOne(context.Background(), view.ID) }()
	<-started
	if err := db.Model(&models.EnvironmentReleaseFence{}).Where("resource = ?", "environment-one").Update("high_water", 2).Error; err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(220 * time.Millisecond):
		t.Fatal("new durable generation did not cancel prior native operation promptly")
	}
	if err := <-done; err == nil {
		t.Fatal("superseded owner published success")
	}
	var row models.EnvironmentRelease
	_ = db.Take(&row, "id = ?", view.ID).Error
	if row.State != "unknown" || native.deletes.Load() != 1 {
		t.Fatal(row.State, native.deletes.Load())
	}
	if _, err := r.Renew(context.Background(), "environment-one", view.ID, EnvironmentReleaseRenew{Owner: "owner-original", Generation: 1, ExpiresAt: time.Now().Add(200 * time.Millisecond)}); !errors.Is(err, ErrReleaseOwner) {
		t.Fatal("unknown owner revived", err)
	}
}

func TestEnvironmentReleaseOriginalReceiptOutlivesLeaseAndLateLifecycleReply(t *testing.T) {
	r, native, db := releaseServiceFixture(t, EnvironmentReleaseOptions{})
	view := confirmedRelease(t, r, "late-reply-release")
	if err := r.RunOne(context.Background(), view.ID); err != nil {
		t.Fatal(err)
	}
	var row models.EnvironmentRelease
	_ = db.Take(&row, "id = ?", view.ID).Error
	old := EnvironmentReleaseConfirm{PlanSHA256: row.PlanSHA256, Nonce: row.ConfirmNonce, Confirmed: true, LossAccepted: true, Owner: row.Owner, Generation: row.Generation, ExpiresAt: time.Now().Add(-time.Hour), Deadline: time.Now().Add(-time.Minute)}
	got, err := r.Confirm(context.Background(), row.Resource, row.ID, old)
	if err != nil || got.State != "released" || got.Receipt == nil {
		t.Fatal("expired duplicate lost original receipt", got, err)
	}
	r.sandbox.setStatus(row.Resource, models.StatusRunning, "")
	var sb models.Sandbox
	_ = db.Take(&sb, "id = ?", row.Resource).Error
	if sb.Status != "released" {
		t.Fatal("late running status resurrected released metadata")
	}
	if native.deletes.Load() != 1 {
		t.Fatal("late receipt dispatched twice")
	}
}

func TestEnvironmentReleaseCapabilityAndDurableIntentIdentity(t *testing.T) {
	var absent *EnvironmentReleases
	if absent.Capabilities(t.Context(), "environment-one").Supported {
		t.Fatal("nil controller advertised support")
	}
	if _, err := absent.Preview(t.Context(), "environment-one", EnvironmentReleasePrepare{}); !errors.Is(err, driver.ErrEnvironmentReleaseUnsupported) {
		t.Fatal(err)
	}
	if _, err := absent.Confirm(t.Context(), "environment-one", "release", EnvironmentReleaseConfirm{}); !errors.Is(err, driver.ErrEnvironmentReleaseUnsupported) {
		t.Fatal(err)
	}
	r, native, db := releaseServiceFixture(t, EnvironmentReleaseOptions{})
	if r.Capabilities(t.Context(), "missing").Reason != "resource_metadata_unavailable" {
		t.Fatal("missing resource advertised release")
	}
	var sb models.Sandbox
	_ = db.Take(&sb, "id = ?", "environment-one").Error
	sb.SetEnv(map[string]string{})
	_ = db.Save(&sb).Error
	if r.Capabilities(t.Context(), sb.ID).Reason != "execution_lineage_unavailable" {
		t.Fatal("missing lineage advertised release")
	}
	sb.SetEnv(map[string]string{"GRASP_EXECUTION_LINEAGE": "lineage-one"})
	_ = db.Save(&sb).Error
	for _, in := range []EnvironmentReleasePrepare{{ID: "../host", Lineage: "lineage-one"}, {ID: "valid", Lineage: "different-lineage"}} {
		if _, err := r.Preview(t.Context(), sb.ID, in); err == nil {
			t.Fatal("forged metadata accepted")
		}
	}
	view := confirmedRelease(t, r, "durable-intent")
	if _, err := r.Preview(t.Context(), sb.ID, EnvironmentReleasePrepare{ID: view.ID, Lineage: "changed-lineage"}); !errors.Is(err, ErrReleaseConflict) {
		t.Fatal("receipt silently changed lineage", err)
	}
	if _, err := r.Preview(t.Context(), sb.ID, EnvironmentReleasePrepare{ID: "other-intent", Lineage: "lineage-one"}); !errors.Is(err, driver.ErrEnvironmentReleasePending) {
		t.Fatal("second intent crossed permanent barrier", err)
	}
	if _, err := r.Get(t.Context(), "other-resource", view.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("operation escaped its physical resource", err)
	}
	if native.deletes.Load() != 0 {
		t.Fatal("metadata path deleted resource")
	}
}
func TestEnvironmentReleaseConfirmationCannotReplaceFrozenPlanOrNonce(t *testing.T) {
	for _, change := range []string{"hash", "expired-lease", "far-lease", "expired-deadline", "far-deadline", "corrupt-plan", "expired-preview", "changed-lineage", "wrong-generation"} {
		t.Run(change, func(t *testing.T) {
			r, native, db := releaseServiceFixture(t, EnvironmentReleaseOptions{})
			view, err := r.Preview(t.Context(), "environment-one", EnvironmentReleasePrepare{ID: "frozen-confirm", Lineage: "lineage-one"})
			if err != nil {
				t.Fatal(err)
			}
			in := EnvironmentReleaseConfirm{PlanSHA256: view.Plan.SHA256, Nonce: "manual-confirm", Confirmed: true, LossAccepted: true, Owner: "owner-original", Generation: 1, ExpiresAt: time.Now().Add(40 * time.Second), Deadline: time.Now().Add(time.Minute)}
			switch change {
			case "hash":
				in.PlanSHA256 = "other"
			case "expired-lease":
				in.ExpiresAt = time.Now().Add(-time.Second)
			case "far-lease":
				in.ExpiresAt = time.Now().Add(time.Hour)
			case "expired-deadline":
				in.Deadline = time.Now().Add(-time.Second)
			case "far-deadline":
				in.Deadline = time.Now().Add(time.Hour)
			case "corrupt-plan":
				_ = db.Model(&models.EnvironmentRelease{}).Where("id = ?", view.ID).Update("plan", "{}").Error
			case "expired-preview":
				_ = db.Model(&models.EnvironmentRelease{}).Where("id = ?", view.ID).Update("deadline", time.Now().Add(-time.Second)).Error
			case "changed-lineage":
				var sb models.Sandbox
				_ = db.Take(&sb, "id = ?", "environment-one").Error
				sb.SetEnv(map[string]string{"GRASP_EXECUTION_LINEAGE": "other"})
				_ = db.Save(&sb).Error
			case "wrong-generation":
				_ = db.Model(&models.EnvironmentReleaseFence{}).Where("resource = ?", "environment-one").Update("high_water", 1).Error
			}
			if _, err := r.Confirm(t.Context(), "environment-one", view.ID, in); err == nil {
				t.Fatal("stale or replaced intent accepted")
			}
			if native.deletes.Load() != 0 {
				t.Fatal("rejected intent deleted resources")
			}
		})
	}
	r, _, _ := releaseServiceFixture(t, EnvironmentReleaseOptions{})
	view := confirmedRelease(t, r, "nonce-confirm")
	original := EnvironmentReleaseConfirm{PlanSHA256: view.Plan.SHA256, Nonce: "different-nonce", Confirmed: true, LossAccepted: true, Owner: "owner-original", Generation: 1, ExpiresAt: time.Now().Add(time.Second), Deadline: time.Now().Add(time.Minute)}
	if _, err := r.Confirm(t.Context(), "environment-one", view.ID, original); !errors.Is(err, ErrReleaseConflict) {
		t.Fatal("nonce changed accepted loss intent", err)
	}
}
func TestEnvironmentReleaseReadOnlyCompletionRequiresOriginalPhysicalFence(t *testing.T) {
	for _, change := range []string{"plan", "fence", "lineage"} {
		t.Run(change, func(t *testing.T) {
			r, native, db := releaseServiceFixture(t, EnvironmentReleaseOptions{})
			view := confirmedRelease(t, r, "completion-original")
			now := time.Now().UTC()
			_ = db.Model(&models.EnvironmentRelease{}).Where("id = ?", view.ID).Updates(map[string]any{"state": "unknown", "dispatched_at": now}).Error
			native.gone = true
			switch change {
			case "plan":
				_ = db.Model(&models.EnvironmentRelease{}).Where("id = ?", view.ID).Update("plan_sha256", "different").Error
			case "fence":
				_ = db.Model(&models.EnvironmentReleaseFence{}).Where("resource = ?", "environment-one").Update("high_water", 2).Error
			case "lineage":
				var sb models.Sandbox
				_ = db.Take(&sb, "id = ?", "environment-one").Error
				sb.SetEnv(map[string]string{"GRASP_EXECUTION_LINEAGE": "different"})
				_ = db.Save(&sb).Error
			}
			if _, err := r.Get(t.Context(), "environment-one", view.ID); !errors.Is(err, ErrReleaseConflict) {
				t.Fatal("read completion crossed physical fence", err)
			}
			var row models.EnvironmentRelease
			_ = db.Take(&row, "id = ?", view.ID).Error
			if row.State == "released" || native.deletes.Load() != 0 {
				t.Fatal("read publication ignored original proof")
			}
		})
	}
}
func TestEnvironmentReleaseApplicationWorkerOwnsDispatchAndObservesReceipt(t *testing.T) {
	r, native, _ := releaseServiceFixture(t, EnvironmentReleaseOptions{PollInterval: 5 * time.Millisecond})
	view := confirmedRelease(t, r, "application-worker-release")
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		got, err := r.Get(t.Context(), "environment-one", view.ID)
		if err == nil && got.State == "released" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	got, err := r.Get(t.Context(), "environment-one", view.ID)
	if err != nil || got.State != "released" || native.deletes.Load() != 1 {
		t.Fatal(got, err, native.deletes.Load())
	}
}
