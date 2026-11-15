package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sandbox-gateway/internal/config"
	"sandbox-gateway/internal/database"
	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/driver/fake"
	"sandbox-gateway/internal/models"
	"sandbox-gateway/internal/store"

	"gorm.io/gorm"
)

// changingEndpointDriver gives every incarnation a different SSH endpoint. The
// session listener stays real so recreate still has to pass the readiness probe.
type changingEndpointDriver struct {
	*fake.Driver
	generation atomic.Int32
}

// A provider timeout can return an error after creating the actual workload.
// Cold-recovery eligibility is not proof that this new resource was released.
type partialRestoreDriver struct {
	*fake.Driver
	failCreate atomic.Bool
	failDelete atomic.Bool
}

func (d *partialRestoreDriver) Create(ctx context.Context, spec driver.Spec) (*driver.Handle, error) {
	h, err := d.Driver.Create(ctx, spec)
	if err == nil && d.failCreate.Load() {
		return nil, errors.New("provider response lost after allocation")
	}
	return h, err
}

func (d *partialRestoreDriver) Destroy(ctx context.Context, id string) error {
	if d.failDelete.Load() {
		return errors.New("provider cleanup unavailable")
	}
	return d.Driver.Destroy(ctx, id)
}

func TestPartialColdRestoreNeverReusesPreviousReleaseProof(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(map[bool]string{false: "delete-failure", true: "retry-recovery"}[retry], func(t *testing.T) {
			svc, base, _ := testService(t)
			sb, err := svc.Create(t.Context(), CreateRequest{})
			if err != nil {
				t.Fatal(err)
			}
			waitSvcRunning(t, svc, sb.ID)
			if err := svc.Stop(t.Context(), sb.ID, 1); err != nil {
				t.Fatal(err)
			}
			drv := &partialRestoreDriver{Driver: base}
			drv.failCreate.Store(true)
			svc.drv = drv
			if err := svc.StartPrepared(t.Context(), sb.ID, driver.StartOptions{}); err != nil {
				t.Fatal(err)
			}
			waitSvcError(t, svc, sb.ID)
			if live, _ := base.Status(t.Context(), sb.ID); live != driver.StatusRunning {
				t.Fatal("fixture failed to leave a partially created live workload")
			}
			if retry {
				drv.failCreate.Store(false)
				if err := svc.StartPrepared(t.Context(), sb.ID, driver.StartOptions{}); err != nil {
					t.Fatal(err)
				}
				waitSvcRunning(t, svc, sb.ID)
				return
			}
			drv.failDelete.Store(true)
			if err := svc.Destroy(t.Context(), sb.ID); err == nil {
				t.Fatal("unconfirmed deletion succeeded")
			}
			row, err := svc.Get(sb.ID)
			if err != nil || row.Status != models.StatusDeleting || row.WorkloadReleased {
				t.Fatalf("old stop proof released a newly allocated workload: %+v %v", row, err)
			}
			if status, err := svc.Status(t.Context(), sb.ID); err != nil || status != driver.StatusPending {
				t.Fatalf("unconfirmed deletion returned free capacity: %s %v", status, err)
			}
			drv.failDelete.Store(false)
			if err := svc.Destroy(t.Context(), sb.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func (d *changingEndpointDriver) Create(ctx context.Context, spec driver.Spec) (*driver.Handle, error) {
	d.generation.Add(1)
	h, err := d.Driver.Create(ctx, spec)
	if err == nil {
		h.Endpoints, err = d.Endpoints(ctx, spec.ID)
	}
	return h, err
}

func (d *changingEndpointDriver) Endpoints(ctx context.Context, id string) (map[int]string, error) {
	eps, err := d.Driver.Endpoints(ctx, id)
	if err != nil {
		return nil, err
	}
	copy := make(map[int]string, len(eps))
	for port, address := range eps {
		copy[port] = address
	}
	copy[22] = fmt.Sprintf("127.0.0.1:%d", 32000+d.generation.Load())
	return copy, nil
}

func TestColdStopRecreatesStableIDFromDurableSpecAndFreshEndpoints(t *testing.T) {
	svc, base, st := testService(t)
	drv := &changingEndpointDriver{Driver: base}
	svc.drv = drv
	svc.cfg.InternalPorts = []int{9222}
	sb, err := svc.Create(t.Context(), CreateRequest{
		Image: "custom-image:pinned", WorkspaceDir: "/workspace/custom", Ports: []int{5173},
		Mounts: []string{"/host/shared:/shared:ro", "/host/config:/config"},
		Config: &driver.ConfigInject{HostPath: "/host/agent", ConfigRoot: "/root/.agent"},
		Env:    map[string]string{"GRASP_RUNTIME_URL": "http://old.invalid/runtime", "SANDBOX_INJECT": "http://expired.invalid/config", "SSH_KEY": "retained-key"},
		Labels: map[string]string{"owner": "test"}, CPUCores: 4, MemoryMB: 8192, DiskGi: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	first, err := svc.Get(sb.ID)
	if err != nil {
		t.Fatal(err)
	}
	original := *base.LastCreate()
	if err := svc.Stop(t.Context(), sb.ID, 1); err != nil {
		t.Fatal(err)
	}
	if live, _ := base.Status(t.Context(), sb.ID); live != driver.StatusNotFound {
		t.Fatalf("stop retained the workload: %s", live)
	}
	// A process restart and changed defaults must not invalidate a saved sandbox.
	restarted := New(drv, st, Config{Image: "different-default", Ports: []int{8765}, SessionPort: 8765,
		WorkspaceDir: "/different/default", FinalizeTimeout: 2 * time.Second, Resources: testResources()})
	restarted.ReconcileOnStartup(t.Context())
	stopped, err := restarted.Get(sb.ID)
	if err != nil || stopped.Status != models.StatusStopped || !stopped.WorkloadReleased || stopped.Error != "" {
		t.Fatalf("reconcile discarded stopped recovery state: %+v %v", stopped, err)
	}
	if status, err := restarted.Status(t.Context(), sb.ID); err != nil || status != driver.StatusStopped {
		t.Fatalf("released sandbox observed as missing: %s %v", status, err)
	}
	opts := driver.StartOptions{RuntimeURL: "http://fresh.invalid/runtime", RuntimeHeaders: "Authorization: Bearer fresh"}
	if err := restarted.StartPrepared(t.Context(), sb.ID, opts); err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, restarted, sb.ID)
	got, err := restarted.Get(sb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != sb.ID || got.WorkloadReleased || got.Endpoints()[22] == first.Endpoints()[22] {
		t.Fatalf("recreation lost identity or kept stale endpoints: %+v", got)
	}
	created := base.LastCreate()
	if created.ID != original.ID || created.Image != original.Image || created.WorkspaceDir != original.WorkspaceDir ||
		!reflect.DeepEqual(created.Mounts, original.Mounts) || !reflect.DeepEqual(created.Config, original.Config) ||
		!reflect.DeepEqual(created.Ports, original.Ports) || !reflect.DeepEqual(created.InternalPorts, original.InternalPorts) ||
		!reflect.DeepEqual(created.Labels, original.Labels) || created.Resources != original.Resources {
		t.Fatalf("recreated with different source inputs:\noriginal=%+v\ncreated=%+v", original, created)
	}
	if created.Env["SSH_KEY"] != "retained-key" || created.Env["GRASP_RUNTIME_URL"] != opts.RuntimeURL ||
		created.Env["SANDBOX_INJECT_HEADERS"] != opts.RuntimeHeaders || created.Env["SANDBOX_INJECT"] != "" {
		t.Fatalf("recreation lost grants or reused expired bootstrap inputs: %+v", created.Env)
	}
}

func TestColdResumeDropsExpiredURLConfigAndKeepsRecoveryStateOnCreateFailure(t *testing.T) {
	svc, base, _ := testService(t)
	sb, err := svc.Create(t.Context(), CreateRequest{Config: &driver.ConfigInject{BundleURL: "http://expired.invalid/config", Headers: "expired"},
		Env: map[string]string{"GRASP_RUNTIME_URL": "http://old.invalid/runtime"}})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	if err := svc.Stop(t.Context(), sb.ID, 1); err != nil {
		t.Fatal(err)
	}
	failing := &failCreateDriver{Driver: base, createErr: errors.New("provider unavailable")}
	svc.drv = failing
	if err := svc.StartPrepared(t.Context(), sb.ID, driver.StartOptions{RuntimeURL: "http://fresh.invalid/runtime", RestoreEnv: map[string]string{
		"SANDBOX_INJECT": "http://fresh.invalid/config", "SANDBOX_INJECT_HEADERS": "fresh-grant", "NEW_TOOL_TOKEN": "fresh-token",
	}}); err != nil {
		t.Fatal(err)
	}
	if reason := waitSvcError(t, svc, sb.ID); reason == "" {
		t.Fatal("missing create failure")
	}
	got, err := svc.Get(sb.ID)
	if err != nil || !got.WorkloadReleased || got.SpecJSON == "" {
		t.Fatalf("lost retry state: %+v %v", got, err)
	}
	failing.createErr = nil
	if err := svc.StartPrepared(t.Context(), sb.ID, driver.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	if spec := base.LastCreate(); spec.Config != nil || spec.Env["SANDBOX_INJECT"] != "http://fresh.invalid/config" ||
		spec.Env["SANDBOX_INJECT_HEADERS"] != "fresh-grant" || spec.Env["NEW_TOOL_TOKEN"] != "fresh-token" {
		t.Fatalf("retry reused expired config: %+v", spec)
	}
}

func TestStopDoesNotReleaseWorkloadWhenSaveIntentFails(t *testing.T) {
	svc, drv, _ := testService(t)
	db, err := database.Open(config.DBConfig{Driver: "sqlite", Path: fmt.Sprintf("file:%s-intent-%d?mode=memory&cache=shared", t.Name(), time.Now().UnixNano())})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	svc.store = store.New(db)
	sb, err := svc.Create(t.Context(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	failure := errors.New("write unavailable")
	if err := db.Callback().Update().Before("gorm:update").Register("test:reject-stop-intent", func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*models.Sandbox); ok && row.Status == models.StatusStopping {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Stop(t.Context(), sb.ID, 1); !errors.Is(err, failure) {
		t.Fatalf("save failure=%v", err)
	}
	if status, err := drv.Status(t.Context(), sb.ID); err != nil || status != driver.StatusRunning {
		t.Fatalf("workload released without durable intent: %s %v", status, err)
	}
	got, err := svc.Get(sb.ID)
	if err != nil || got.Status != models.StatusRunning {
		t.Fatalf("save failure damaged record: %+v %v", got, err)
	}
}

type snapshotServiceDriver struct {
	*fake.Driver
	pauseErr       error
	failAfterPause bool
	keep           int
	calls          int
}

func (d *snapshotServiceDriver) PauseSnapshot(ctx context.Context, id string, keep int) error {
	d.calls++
	d.keep = keep
	if d.pauseErr != nil {
		if d.failAfterPause {
			return errors.Join(d.Driver.Stop(ctx, id), d.pauseErr)
		}
		return d.pauseErr
	}
	return d.Driver.Stop(ctx, id)
}

func TestSnapshotStopFailureRetainsRecoveryAndForwardsKeepOnRetry(t *testing.T) {
	svc, base, _ := testService(t)
	sb, err := svc.Create(t.Context(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	failure := errors.New("snapshot upload failed")
	drv := &snapshotServiceDriver{Driver: base, pauseErr: failure}
	svc.drv = drv
	if err := svc.Stop(t.Context(), sb.ID, 5); !errors.Is(err, failure) {
		t.Fatalf("snapshot failure=%v", err)
	}
	got, err := svc.Get(sb.ID)
	if err != nil || got.Status != models.StatusStopping || got.WorkloadReleased || got.Error == "" {
		t.Fatalf("unconfirmed snapshot reported stopped: %+v %v", got, err)
	}
	if status, _ := base.Status(t.Context(), sb.ID); status != driver.StatusRunning {
		t.Fatalf("failed save destroyed workload: %s", status)
	}
	if err := svc.StartPrepared(t.Context(), sb.ID, driver.StartOptions{}); err == nil {
		t.Fatal("start raced pending snapshot")
	}
	drv.pauseErr = nil
	if err := svc.Stop(t.Context(), sb.ID, 5); err != nil {
		t.Fatal(err)
	}
	got, err = svc.Get(sb.ID)
	if err != nil || got.Status != models.StatusStopped || got.WorkloadReleased || got.Error != "" || drv.keep != 5 || drv.calls != 2 {
		t.Fatalf("retry lost snapshot policy/state: %+v %v keep=%d calls=%d", got, err, drv.keep, drv.calls)
	}
	if err := svc.Stop(t.Context(), sb.ID, 0); err == nil || drv.calls != 2 {
		t.Fatal("invalid retention reached driver")
	}
}

func TestDeleteFailureSurvivesRestartAndReconcileRetriesCleanup(t *testing.T) {
	svc, base, st := testService(t)
	sb, err := svc.Create(t.Context(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	drv := &failOpsDriver{Driver: base, destroyErr: errors.New("provider offline")}
	svc.drv = drv
	if err := svc.Destroy(t.Context(), sb.ID); err == nil {
		t.Fatal("delete failure hidden")
	}
	if err := svc.Stop(t.Context(), sb.ID, 1); err == nil {
		t.Fatal("stop admitted during deletion")
	}
	if err := svc.StartPrepared(t.Context(), sb.ID, driver.StartOptions{}); err == nil {
		t.Fatal("start admitted during deletion")
	}
	restarted := New(drv, st, svc.cfg)
	restarted.ReconcileOnStartup(t.Context())
	got, err := restarted.Get(sb.ID)
	if err != nil || got.Status != models.StatusDeleting {
		t.Fatalf("failed retry lost intent: %+v %v", got, err)
	}
	drv.destroyErr = nil
	restarted.ReconcileOnStartup(t.Context())
	if _, err := restarted.Get(sb.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("successful cleanup kept record: %v", err)
	}
	if status, _ := base.Status(t.Context(), sb.ID); status != driver.StatusNotFound {
		t.Fatalf("cleanup orphaned resource: %s", status)
	}
	if err := restarted.Destroy(t.Context(), sb.ID); err != nil {
		t.Fatalf("duplicate delete failed: %v", err)
	}
}

type blockingReleaseDriver struct {
	*fake.Driver
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (d *blockingReleaseDriver) Destroy(ctx context.Context, id string) error {
	d.once.Do(func() { close(d.entered); <-d.release })
	return d.Driver.Destroy(ctx, id)
}

func TestStopSerializesConcurrentStartAndDelete(t *testing.T) {
	for _, operation := range []string{"start", "delete"} {
		t.Run(operation, func(t *testing.T) {
			svc, base, _ := testService(t)
			sb, err := svc.Create(t.Context(), CreateRequest{})
			if err != nil {
				t.Fatal(err)
			}
			waitSvcRunning(t, svc, sb.ID)
			drv := &blockingReleaseDriver{Driver: base, entered: make(chan struct{}), release: make(chan struct{})}
			svc.drv = drv
			stop := make(chan error, 1)
			go func() { stop <- svc.Stop(t.Context(), sb.ID, 1) }()
			select {
			case <-drv.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("stop did not begin release")
			}
			done := make(chan error, 1)
			go func() {
				if operation == "start" {
					done <- svc.StartPrepared(t.Context(), sb.ID, driver.StartOptions{})
				} else {
					done <- svc.Destroy(t.Context(), sb.ID)
				}
			}()
			select {
			case err := <-done:
				close(drv.release)
				t.Fatalf("%s raced resource release: %v", operation, err)
			case <-time.After(25 * time.Millisecond):
			}
			close(drv.release)
			if err := <-stop; err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("serialized operation deadlocked")
			}
			if operation == "start" {
				waitSvcRunning(t, svc, sb.ID)
				if status, _ := base.Status(t.Context(), sb.ID); status != driver.StatusRunning {
					t.Fatalf("start failed to recreate: %s", status)
				}
			} else {
				if _, err := svc.Get(sb.ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("delete left record: %v", err)
				}
				if status, _ := base.Status(t.Context(), sb.ID); status != driver.StatusNotFound {
					t.Fatalf("delete left resource: %s", status)
				}
			}
		})
	}
}

type blockingRecreateDriver struct {
	*fake.Driver
	block   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (d *blockingRecreateDriver) Create(ctx context.Context, spec driver.Spec) (*driver.Handle, error) {
	if d.block.Load() {
		close(d.entered)
		select {
		case <-d.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return d.Driver.Create(ctx, spec)
}

func TestDeleteWaitsForRecreationAndCannotLeaveResurrectedWorkload(t *testing.T) {
	svc, base, _ := testService(t)
	drv := &blockingRecreateDriver{Driver: base, entered: make(chan struct{}), release: make(chan struct{})}
	svc.drv = drv
	sb, err := svc.Create(t.Context(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	if err := svc.Stop(t.Context(), sb.ID, 1); err != nil {
		t.Fatal(err)
	}
	drv.block.Store(true)
	if err := svc.StartPrepared(t.Context(), sb.ID, driver.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-drv.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("recreate was not started")
	}
	done := make(chan error, 1)
	go func() { done <- svc.Destroy(t.Context(), sb.ID) }()
	select {
	case err := <-done:
		close(drv.release)
		t.Fatalf("delete finished while creation could still publish a resource: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(drv.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delete blocked after recreation completed")
	}
	if _, err := svc.Get(sb.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("delete left/recreated record: %v", err)
	}
	if status, _ := base.Status(t.Context(), sb.ID); status != driver.StatusNotFound {
		t.Fatalf("recreate left orphaned workload: %s", status)
	}
}

type captureSpecDriver struct {
	*fake.Driver
	captureErr error
}

func (d *captureSpecDriver) CaptureSpec(_ context.Context, spec driver.Spec) (driver.Spec, error) {
	if d.captureErr != nil {
		return spec, d.captureErr
	}
	spec.Mounts = []string{"/original/data:/workspace:rw"}
	spec.Config = &driver.ConfigInject{HostPath: "/original/config", ConfigRoot: "/root/.config"}
	return spec, nil
}

func TestStopAdoptsUnrecordedMountsBeforeReleasingExistingWorkload(t *testing.T) {
	svc, base, st := testService(t)
	sb, err := svc.Create(t.Context(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	row, err := svc.Get(sb.ID)
	if err != nil {
		t.Fatal(err)
	}
	row.SpecJSON = ""
	if err := st.Save(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("cannot inspect existing binds")
	drv := &captureSpecDriver{Driver: base, captureErr: failure}
	svc.drv = drv
	if err := svc.Stop(t.Context(), sb.ID, 1); !errors.Is(err, failure) {
		t.Fatalf("ignored inspection failure: %v", err)
	}
	if status, _ := base.Status(t.Context(), sb.ID); status != driver.StatusRunning {
		t.Fatal("released before preserving mount configuration")
	}
	drv.captureErr = nil
	if err := svc.Stop(t.Context(), sb.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartPrepared(t.Context(), sb.ID, driver.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	created := base.LastCreate()
	if !reflect.DeepEqual(created.Mounts, []string{"/original/data:/workspace:rw"}) || created.Config == nil || created.Config.HostPath != "/original/config" {
		t.Fatalf("adopted source configuration lost during recreation: %+v", created)
	}
}

func TestReinstallPreservesOriginalWorkspaceMountsAndConfig(t *testing.T) {
	svc, base, _ := testService(t)
	sb, err := svc.Create(t.Context(), CreateRequest{
		WorkspaceDir: "/custom/workspace", Mounts: []string{"/host/shared:/shared:ro"},
		Config: &driver.ConfigInject{HostPath: "/host/config", ConfigRoot: "/root/.agent"},
		Ports:  []int{5173}, Env: map[string]string{"TOKEN": "retained"}, Labels: map[string]string{"owner": "original"},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	original := base.LastCreate()
	svc.cfg.WorkspaceDir = "/changed/default"
	svc.cfg.Ports = []int{8765, 8888}
	if err := svc.Reinstall(t.Context(), sb.ID, true); err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	spec, preserve, called := base.LastReinstall()
	if !called || !preserve || spec.ID != original.ID || spec.WorkspaceDir != original.WorkspaceDir ||
		!reflect.DeepEqual(spec.Mounts, original.Mounts) || !reflect.DeepEqual(spec.Config, original.Config) ||
		!reflect.DeepEqual(spec.Env, original.Env) || !reflect.DeepEqual(spec.Labels, original.Labels) {
		t.Fatalf("reinstall lost original source configuration: %+v preserve=%v", spec, preserve)
	}
	if err := svc.Stop(t.Context(), sb.ID, 1); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartPrepared(t.Context(), sb.ID, driver.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	if recreated := base.LastCreate(); recreated.WorkspaceDir != original.WorkspaceDir || !reflect.DeepEqual(recreated.Mounts, original.Mounts) || !reflect.DeepEqual(recreated.Config, original.Config) {
		t.Fatalf("stop after reinstall lost persisted spec: %+v", recreated)
	}
}

type blockingReinstallDriver struct {
	*fake.Driver
	entered chan struct{}
	release chan struct{}
}

func (d *blockingReinstallDriver) Reinstall(ctx context.Context, spec driver.Spec, preserve bool) error {
	close(d.entered)
	select {
	case <-d.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return d.Driver.Reinstall(ctx, spec, preserve)
}

func TestDeleteSerializesReinstallAndDoesNotResurrectSandbox(t *testing.T) {
	svc, base, _ := testService(t)
	sb, err := svc.Create(t.Context(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	drv := &blockingReinstallDriver{Driver: base, entered: make(chan struct{}), release: make(chan struct{})}
	svc.drv = drv
	if err := svc.Reinstall(t.Context(), sb.ID, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-drv.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("reinstall not started")
	}
	done := make(chan error, 1)
	go func() { done <- svc.Destroy(t.Context(), sb.ID) }()
	select {
	case err := <-done:
		close(drv.release)
		t.Fatalf("delete raced reinstall: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(drv.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delete did not finish after reinstall")
	}
	if _, err := svc.Get(sb.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("reinstall resurrected record: %v", err)
	}
	if status, _ := base.Status(t.Context(), sb.ID); status != driver.StatusNotFound {
		t.Fatalf("reinstall resurrected workload: %s", status)
	}
}

type blockingFinalizeDriver struct {
	*fake.Driver
	entered chan struct{}
	release chan struct{}
}

func (d *blockingFinalizeDriver) Endpoints(ctx context.Context, id string) (map[int]string, error) {
	close(d.entered)
	select {
	case <-d.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return d.Driver.Endpoints(ctx, id)
}

func TestDeleteWaitsForRetainedStartReadiness(t *testing.T) {
	svc, base, _ := testService(t)
	sb, err := svc.Create(t.Context(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	if err := base.Stop(t.Context(), sb.ID); err != nil {
		t.Fatal(err)
	}
	drv := &blockingFinalizeDriver{Driver: base, entered: make(chan struct{}), release: make(chan struct{})}
	svc.drv = drv
	if err := svc.StartPrepared(t.Context(), sb.ID, driver.StartOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-drv.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("resume did not start readiness")
	}
	done := make(chan error, 1)
	go func() { done <- svc.Destroy(t.Context(), sb.ID) }()
	select {
	case err := <-done:
		close(drv.release)
		t.Fatalf("delete raced resume readiness: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(drv.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delete blocked after readiness completed")
	}
	if _, err := svc.Get(sb.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("resume resurrected record: %v", err)
	}
}

func TestUnconfirmedSnapshotRemainsPendingAcrossObservationAndRestart(t *testing.T) {
	svc, base, st := testService(t)
	sb, err := svc.Create(t.Context(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	drv := &snapshotServiceDriver{Driver: base, pauseErr: errors.New("snapshot upload incomplete"), failAfterPause: true}
	svc.drv = drv
	if err := svc.Stop(t.Context(), sb.ID, 5); err == nil {
		t.Fatal("unconfirmed save succeeded")
	}
	if observed, err := base.Status(t.Context(), sb.ID); err != nil || observed != driver.StatusStopped {
		t.Fatalf("fixture must represent VM paused but snapshot not saved: %s %v", observed, err)
	}
	if status, err := svc.Status(t.Context(), sb.ID); err != nil || status != driver.StatusPending {
		t.Fatalf("read-only status mistook paused VM for committed recovery: %s %v", status, err)
	}
	restarted := New(drv, st, svc.cfg)
	restarted.ReconcileOnStartup(t.Context())
	row, err := restarted.Get(sb.ID)
	if err != nil || row.Status != models.StatusStopping || row.Error == "" {
		t.Fatalf("restart reported successful save without proof: %+v %v", row, err)
	}
	drv.pauseErr = nil
	if err := restarted.Stop(t.Context(), sb.ID, 5); err != nil {
		t.Fatalf("snapshot proof retry failed: %v", err)
	}
	if status, err := restarted.Status(t.Context(), sb.ID); err != nil || status != driver.StatusStopped {
		t.Fatalf("confirmed snapshot stayed pending: %s %v", status, err)
	}
}

func TestReconcileSerializesConcurrentStopAndDelete(t *testing.T) {
	for _, operation := range []string{"stop", "delete"} {
		t.Run(operation, func(t *testing.T) {
			svc, base, _ := testService(t)
			sb, err := svc.Create(t.Context(), CreateRequest{})
			if err != nil {
				t.Fatal(err)
			}
			waitSvcRunning(t, svc, sb.ID)
			drv := &blockingFinalizeDriver{Driver: base, entered: make(chan struct{}), release: make(chan struct{})}
			svc.drv = drv
			reconciled := make(chan struct{})
			go func() { svc.ReconcileOnStartup(t.Context()); close(reconciled) }()
			select {
			case <-drv.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("reconcile did not observe endpoints")
			}
			done := make(chan error, 1)
			go func() {
				if operation == "stop" {
					done <- svc.Stop(t.Context(), sb.ID, 1)
				} else {
					done <- svc.Destroy(t.Context(), sb.ID)
				}
			}()
			select {
			case err := <-done:
				close(drv.release)
				<-reconciled
				t.Fatalf("%s raced reconcile and its stale write: %v", operation, err)
			case <-time.After(25 * time.Millisecond):
			}
			close(drv.release)
			<-reconciled
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("reconcile lifecycle lock deadlocked")
			}
			if operation == "delete" {
				if _, err := svc.Get(sb.ID); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("reconcile resurrected deleted record: %v", err)
				}
			} else {
				row, err := svc.Get(sb.ID)
				if err != nil || row.Status != models.StatusStopped || !row.WorkloadReleased {
					t.Fatalf("reconcile replaced saved stop state: %+v %v", row, err)
				}
			}
			if status, _ := base.Status(t.Context(), sb.ID); status != driver.StatusNotFound {
				t.Fatalf("reconcile retained workload after %s: %s", operation, status)
			}
		})
	}
}

type blockingPublishDriver struct {
	*fake.Driver
	entered chan struct{}
	release chan struct{}
}

func (d *blockingPublishDriver) PublishPort(ctx context.Context, id string, port int) error {
	spec, ok := d.SpecOf(id)
	if !ok {
		return errors.New("sandbox workload not found")
	}
	close(d.entered)
	select {
	case <-d.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	// Like Kubernetes' ensure-Service path, publishing can create resources
	// after an earlier read. It must not run across a completed lifecycle delete.
	spec.Ports = append(append([]int{}, spec.Ports...), port)
	return d.Driver.Reinstall(ctx, spec, true)
}

func TestDeleteSerializesPortPublishAndCannotLeaveOrphanResources(t *testing.T) {
	svc, base, _ := testService(t)
	sb, err := svc.Create(t.Context(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	drv := &blockingPublishDriver{Driver: base, entered: make(chan struct{}), release: make(chan struct{})}
	svc.drv = drv
	published := make(chan error, 1)
	go func() { _, err := svc.PublishPort(t.Context(), sb.ID, 5173); published <- err }()
	select {
	case <-drv.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("port publish did not start")
	}
	done := make(chan error, 1)
	go func() { done <- svc.Destroy(t.Context(), sb.ID) }()
	select {
	case err := <-done:
		close(drv.release)
		<-published
		t.Fatalf("delete raced resource publication: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(drv.release)
	if err := <-published; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delete blocked after port publication completed")
	}
	if _, err := svc.Get(sb.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("port publish resurrected record: %v", err)
	}
	if status, _ := base.Status(t.Context(), sb.ID); status != driver.StatusNotFound {
		t.Fatalf("port publish resurrected deleted resources: %s", status)
	}
}

type selectiveReconcileDriver struct {
	*fake.Driver
	blockID string
	entered chan struct{}
	release chan struct{}
}

func (d *selectiveReconcileDriver) Endpoints(ctx context.Context, id string) (map[int]string, error) {
	if id == d.blockID {
		close(d.entered)
		select {
		case <-d.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return d.Driver.Endpoints(ctx, id)
}

func TestReconcileReloadsEachRecordAfterReadingStartupInventory(t *testing.T) {
	svc, base, _ := testService(t)
	for range 2 {
		sb, err := svc.Create(t.Context(), CreateRequest{})
		if err != nil {
			t.Fatal(err)
		}
		waitSvcRunning(t, svc, sb.ID)
	}
	rows, err := svc.List(ListFilter{})
	if err != nil || len(rows) != 2 {
		t.Fatalf("inventory=%+v %v", rows, err)
	}
	drv := &selectiveReconcileDriver{Driver: base, blockID: rows[0].ID, entered: make(chan struct{}), release: make(chan struct{})}
	svc.drv = drv
	done := make(chan struct{})
	go func() { svc.ReconcileOnStartup(t.Context()); close(done) }()
	select {
	case <-drv.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("reconcile did not begin first inventory record")
	}
	// The second row is in reconcile's original inventory, but has not yet
	// acquired its lifecycle lock. Its deletion must win over that stale copy.
	if err := svc.Destroy(t.Context(), rows[1].ID); err != nil {
		close(drv.release)
		<-done
		t.Fatal(err)
	}
	close(drv.release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reconcile deadlocked after inventory changed")
	}
	if _, err := svc.Get(rows[1].ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("stale startup inventory resurrected deleted record: %v", err)
	}
}

func TestPublishPortRejectsUnavailableSandboxEvenWithCachedEndpoint(t *testing.T) {
	for _, state := range []string{models.StatusStopping, models.StatusDeleting, models.StatusStopped} {
		t.Run(state, func(t *testing.T) {
			svc, _, st := testService(t)
			sb, err := svc.Create(t.Context(), CreateRequest{})
			if err != nil {
				t.Fatal(err)
			}
			waitSvcRunning(t, svc, sb.ID)
			sb, err = svc.Get(sb.ID)
			if err != nil {
				t.Fatal(err)
			}
			sb.Status = state
			sb.WorkloadReleased = state == models.StatusStopped
			if err := st.Save(t.Context(), sb); err != nil {
				t.Fatal(err)
			}
			if endpoint, err := svc.PublishPort(t.Context(), sb.ID, 8765); err == nil || endpoint != "" {
				t.Fatalf("unavailable sandbox returned stale endpoint: %q %v", endpoint, err)
			}
		})
	}
}

type cleanupProofDriver struct {
	*fake.Driver
	cleanupErr error
}

func (d *cleanupProofDriver) Destroy(ctx context.Context, id string) error {
	var pending *driver.CleanupPendingError
	if d.cleanupErr == nil || (errors.As(d.cleanupErr, &pending) && pending.WorkloadReleased) {
		if err := d.Driver.Destroy(ctx, id); err != nil {
			return err
		}
	}
	return d.cleanupErr
}

func TestPendingCleanupReleasesCapacityOnlyAfterConfirmedRuntimeExit(t *testing.T) {
	for _, released := range []bool{false, true} {
		t.Run(fmt.Sprintf("runtime-released-%t", released), func(t *testing.T) {
			svc, base, st := testService(t)
			sb, err := svc.Create(t.Context(), CreateRequest{})
			if err != nil {
				t.Fatal(err)
			}
			waitSvcRunning(t, svc, sb.ID)
			drv := &cleanupProofDriver{Driver: base, cleanupErr: &driver.CleanupPendingError{WorkloadReleased: released}}
			svc.drv = drv
			if err := svc.Destroy(t.Context(), sb.ID); err == nil {
				t.Fatal("pending disk cleanup was reported completed")
			}
			row, err := svc.Get(sb.ID)
			if err != nil || row.Status != models.StatusDeleting || row.WorkloadReleased != released || row.Error == "" {
				t.Fatalf("cleanup lost durable runtime proof or retry identity: %+v %v", row, err)
			}
			expected := driver.StatusPending
			if released {
				expected = driver.StatusStopped
			}
			if status, err := svc.Status(t.Context(), sb.ID); err != nil || status != expected {
				t.Fatalf("capacity-facing state=%s, want %s: %v", status, expected, err)
			}
			if err := svc.StartPrepared(t.Context(), sb.ID, driver.StartOptions{}); err == nil {
				t.Fatal("pending delete could be restored as a stopped environment")
			}
			// A transient provider outage after confirmed exit must not recreate
			// runtime allocation, nor discard the remaining disk cleanup intent.
			drv.cleanupErr = errors.New("provider unavailable")
			restarted := New(drv, st, svc.cfg)
			restarted.ReconcileOnStartup(t.Context())
			row, err = restarted.Get(sb.ID)
			if err != nil || row.Status != models.StatusDeleting || row.WorkloadReleased != released {
				t.Fatalf("restart lost cleanup state: %+v %v", row, err)
			}
			if status, err := restarted.Status(t.Context(), sb.ID); err != nil || status != expected {
				t.Fatalf("restart discarded confirmed capacity state: %s %v", status, err)
			}
			drv.cleanupErr = nil
			restarted.retryDeletes(t.Context())
			if _, err := restarted.Get(sb.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("successful disk cleanup retained the retry record: %v", err)
			}
			if status, _ := base.Status(t.Context(), sb.ID); status != driver.StatusNotFound {
				t.Fatalf("cleanup retained runtime: %s", status)
			}
		})
	}
}
