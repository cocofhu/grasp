package service

import (
	"context"
	"errors"
	"fmt"
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

func provisioningService(t *testing.T) (*SandboxService, *fake.Driver, *gorm.DB) {
	t.Helper()
	db, err := database.Open(config.DBConfig{Driver: "sqlite", Path: fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", t.Name(), time.Now().UnixNano())})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	drv := fake.New()
	if err := drv.WithSessionListener(8765); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drv.Close)
	svc := New(drv, store.New(db), Config{Image: "test-image:local", Ports: []int{8765, 22},
		SessionPort: 8765, WorkspaceDir: "/root/workspace", FinalizeTimeout: 2 * time.Second, Resources: testResources()})
	return svc, drv, db
}

func assertProvisioningFinished(t *testing.T, svc *SandboxService, id string) {
	t.Helper()
	unlock := svc.lockLifecycle(id)
	defer unlock()
	if _, active := svc.provisioning.Load(id); active {
		t.Fatal("finished provisioning still suppresses startup recovery")
	}
}

func TestReconcileCannotFailCreateBeforeAsyncProvisioningStarts(t *testing.T) {
	svc, drv, db := provisioningService(t)
	persisted := make(chan string, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	if err := db.Callback().Create().After("gorm:commit_or_rollback_transaction").Register("test:hold-create-handoff", func(tx *gorm.DB) {
		if sb, ok := tx.Statement.Dest.(*models.Sandbox); ok && tx.Error == nil {
			persisted <- sb.ID
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := svc.Create(t.Context(), CreateRequest{}); done <- err }()
	var id string
	select {
	case id = <-persisted:
	case <-time.After(2 * time.Second):
		t.Fatal("create did not commit its record")
	}
	svc.ReconcileOnStartup(t.Context())
	row, err := svc.Get(id)
	if err != nil || row.Status != models.StatusCreating || row.Error != "" {
		t.Fatalf("startup recovery invalidated an accepted create: %+v %v", row, err)
	}
	if status, _ := drv.Status(t.Context(), id); status != driver.StatusNotFound {
		t.Fatalf("fixture did not hold the create handoff: %s", status)
	}
	releaseOnce.Do(func() { close(release) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, id)
	assertProvisioningFinished(t, svc, id)
}

type handoffDriver struct {
	*fake.Driver
	operation string
	entered   chan struct{}
	release   chan struct{}
	once      sync.Once
}

func (d *handoffDriver) wait(ctx context.Context) error {
	d.once.Do(func() { close(d.entered) })
	select {
	case <-d.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *handoffDriver) Create(ctx context.Context, spec driver.Spec) (*driver.Handle, error) {
	if d.operation == "cold-start" {
		if err := d.wait(ctx); err != nil {
			return nil, err
		}
	}
	return d.Driver.Create(ctx, spec)
}

func (d *handoffDriver) Endpoints(ctx context.Context, id string) (map[int]string, error) {
	if d.operation == "retained-start" {
		if err := d.wait(ctx); err != nil {
			return nil, err
		}
	}
	return d.Driver.Endpoints(ctx, id)
}

func (d *handoffDriver) Reinstall(ctx context.Context, spec driver.Spec, preserve bool) error {
	if err := d.wait(ctx); err != nil {
		return err
	}
	return d.Driver.Reinstall(ctx, spec, preserve)
}

func TestReconcileCannotTakeOverStartOrReinstallHandoff(t *testing.T) {
	for _, operation := range []string{"cold-start", "retained-start", "reinstall"} {
		t.Run(operation, func(t *testing.T) {
			svc, base, db := provisioningService(t)
			sb, err := svc.Create(t.Context(), CreateRequest{})
			if err != nil {
				t.Fatal(err)
			}
			waitSvcRunning(t, svc, sb.ID)
			assertProvisioningFinished(t, svc, sb.ID)
			if operation == "cold-start" {
				if err := svc.Stop(t.Context(), sb.ID, 1); err != nil {
					t.Fatal(err)
				}
			} else if operation == "retained-start" {
				if err := base.Stop(t.Context(), sb.ID); err != nil {
					t.Fatal(err)
				}
			}
			drv := &handoffDriver{Driver: base, operation: operation, entered: make(chan struct{}), release: make(chan struct{})}
			svc.drv = drv
			persisted, release := make(chan struct{}), make(chan struct{})
			var once atomic.Bool
			var releaseOnce, driverReleaseOnce sync.Once
			t.Cleanup(func() {
				releaseOnce.Do(func() { close(release) })
				driverReleaseOnce.Do(func() { close(drv.release) })
			})
			if err := db.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("test:hold-resume-handoff", func(tx *gorm.DB) {
				if row, ok := tx.Statement.Dest.(*models.Sandbox); ok && row.Status == models.StatusCreating && tx.Error == nil && once.CompareAndSwap(false, true) {
					close(persisted)
					<-release
				}
			}); err != nil {
				t.Fatal(err)
			}
			inventoryRead := make(chan struct{})
			var inventoryOnce sync.Once
			if err := db.Callback().Query().After("gorm:query").Register("test:observe-reconcile-inventory", func(tx *gorm.DB) {
				if _, ok := tx.Statement.Dest.(*[]models.Sandbox); ok {
					inventoryOnce.Do(func() { close(inventoryRead) })
				}
			}); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				if operation == "reinstall" {
					done <- svc.Reinstall(t.Context(), sb.ID, true)
				} else {
					done <- svc.StartPrepared(t.Context(), sb.ID, driver.StartOptions{})
				}
			}()
			select {
			case <-persisted:
			case <-time.After(2 * time.Second):
				t.Fatal("operation did not persist the handoff")
			}
			reconciled := make(chan struct{})
			go func() { svc.ReconcileOnStartup(t.Context()); close(reconciled) }()
			select {
			case <-inventoryRead:
			case <-time.After(2 * time.Second):
				t.Fatal("reconcile did not read the handoff record")
			}
			// The accepted operation owns this handoff. Startup recovery must
			// finish without waiting for its slow provider/readiness work.
			select {
			case <-reconciled:
			case <-time.After(time.Second):
				t.Fatal("startup recovery waited for an active provisioning handoff")
			}
			releaseOnce.Do(func() { close(release) })
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			select {
			case <-drv.entered:
			case <-time.After(time.Second):
				t.Fatal("reconcile replaced creating status and suppressed the requested operation")
			}
			row, err := svc.Get(sb.ID)
			if err != nil || row.Status != models.StatusCreating {
				t.Fatalf("operation reported ready before its readiness completed: %+v %v", row, err)
			}
			driverReleaseOnce.Do(func() { close(drv.release) })
			waitSvcRunning(t, svc, sb.ID)
			assertProvisioningFinished(t, svc, sb.ID)
		})
	}
}

func TestFailedProvisioningPersistenceDoesNotSuppressLaterReconcile(t *testing.T) {
	for _, operation := range []string{"create", "cold-start", "retained-start", "reinstall"} {
		t.Run(operation, func(t *testing.T) {
			svc, base, db := provisioningService(t)
			var id string
			if operation != "create" {
				sb, err := svc.Create(t.Context(), CreateRequest{})
				if err != nil {
					t.Fatal(err)
				}
				id = sb.ID
				waitSvcRunning(t, svc, id)
				assertProvisioningFinished(t, svc, id)
				if operation == "cold-start" {
					if err := svc.Stop(t.Context(), id, 1); err != nil {
						t.Fatal(err)
					}
				} else if operation == "retained-start" {
					if err := base.Stop(t.Context(), id); err != nil {
						t.Fatal(err)
					}
				}
			}
			failure := errors.New("cannot persist provisioning")
			reject := func(tx *gorm.DB) {
				if sb, ok := tx.Statement.Dest.(*models.Sandbox); ok && sb.Status == models.StatusCreating {
					id = sb.ID
					tx.AddError(failure)
				}
			}
			if err := db.Callback().Create().Before("gorm:create").Register("test:reject-create", reject); err != nil {
				t.Fatal(err)
			}
			if err := db.Callback().Update().Before("gorm:update").Register("test:reject-start", reject); err != nil {
				t.Fatal(err)
			}
			var err error
			switch operation {
			case "create":
				_, err = svc.Create(t.Context(), CreateRequest{})
			case "reinstall":
				err = svc.Reinstall(t.Context(), id, true)
			default:
				err = svc.StartPrepared(t.Context(), id, driver.StartOptions{})
			}
			if !errors.Is(err, failure) {
				t.Fatalf("persistence failure lost: %v", err)
			}
			assertProvisioningFinished(t, svc, id)
		})
	}
}

func TestReconcileStillRecoversProvisioningLeftByCrashedProcess(t *testing.T) {
	for _, cold := range []bool{false, true} {
		for _, state := range []string{models.StatusCreating, models.StatusPulling} {
			t.Run(fmt.Sprintf("cold-%t-%s", cold, state), func(t *testing.T) {
				svc, drv, st := testService(t)
				sb, err := svc.Create(t.Context(), CreateRequest{})
				if err != nil {
					t.Fatal(err)
				}
				waitSvcRunning(t, svc, sb.ID)
				assertProvisioningFinished(t, svc, sb.ID)
				sb, err = svc.Get(sb.ID)
				if err != nil {
					t.Fatal(err)
				}
				originalSpec := sb.SpecJSON
				sb.Status, sb.WorkloadReleased = state, cold
				if err := st.Save(t.Context(), sb); err != nil {
					t.Fatal(err)
				}
				if !cold {
					if err := drv.Destroy(t.Context(), sb.ID); err != nil {
						t.Fatal(err)
					}
				}
				// A new process has no active attempt. Even a leftover cold
				// workload being alive does not prove its files were restored.
				restarted := New(drv, st, svc.cfg)
				restarted.ReconcileOnStartup(t.Context())
				row, err := restarted.Get(sb.ID)
				if err != nil || row.Status != models.StatusError || row.Error == "" || row.SpecJSON != originalSpec || row.WorkloadReleased != cold {
					t.Fatalf("crashed operation was hidden or declared ready: %+v %v", row, err)
				}
				if cold {
					if err := restarted.StartPrepared(t.Context(), sb.ID, driver.StartOptions{}); err != nil {
						t.Fatal(err)
					}
					waitSvcRunning(t, restarted, sb.ID)
					assertProvisioningFinished(t, restarted, sb.ID)
				}
			})
		}
	}
}
