package services

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

func TestSharedCapacityAcrossIndependentServiceConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared-capacity.db")
	var services []*SandboxService
	for range 2 {
		db, err := database.OpenSQLiteTest(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close(db) })
		services = append(services, NewSandboxService(db, nil, nil, nil, SandboxOptions{Max: 3}))
	}
	results := make(chan error, 20)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			results <- services[i%2].createSandboxRow(&models.Sandbox{Name: fmt.Sprintf("multipool-%d", i), Status: "creating", Purpose: "test"})
		})
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
			continue
		}
		var limit ErrSandboxLimit
		if !errors.As(err, &limit) {
			t.Fatal(err)
		}
	}
	if accepted != 3 {
		t.Fatalf("independent connections admitted %d, want 3", accepted)
	}
}

func TestSharedCapacityConcurrentPurposes(t *testing.T) {
	db := newTestDB(t)
	svc := NewSandboxService(db, nil, nil, nil, SandboxOptions{Max: 3})
	var wg sync.WaitGroup
	results := make(chan error, 30)
	for i := range 30 {
		wg.Go(func() {
			purpose := []string{"test", SandboxPurposeSolo, "run"}[i%3]
			results <- svc.createSandboxRow(&models.Sandbox{Name: fmt.Sprintf("capacity-%d", i), Purpose: purpose, Status: "creating"})
		})
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
			continue
		}
		var limit ErrSandboxLimit
		if !errors.As(err, &limit) {
			t.Fatal(err)
		}
	}
	if success != 3 {
		t.Fatalf("admitted %d, want 3", success)
	}
	svc.SetMaxSandboxes(1)
	capacity, err := svc.Capacity()
	if err != nil || capacity.Used != 3 || capacity.Limit != 1 {
		t.Fatalf("lowering cap evicted resources: %+v %v", capacity, err)
	}
}

func TestSharedCapacityResumeAndUncertainRelease(t *testing.T) {
	db := newTestDB(t)
	svc := NewSandboxService(db, nil, nil, nil, SandboxOptions{Max: 1})
	row := models.Sandbox{Name: "restore", Status: "stopped", Purpose: SandboxPurposeSolo}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	release, err := svc.reserveSandbox(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.createSandboxRow(&models.Sandbox{Name: "new", Status: "creating", Purpose: "run"}); err == nil {
		t.Fatal("creation raced past recovery reservation")
	}
	release(false)
	release(true) // a completed callback must never re-acquire the slot
	c, err := svc.Capacity()
	if err != nil || c.Used != 0 {
		t.Fatalf("unallocated reservation did not roll back: %+v %v", c, err)
	}
	release, err = svc.reserveSandbox(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&row).Update("status", "error").Error; err != nil {
		t.Fatal(err)
	}
	release(false)
	c, err = svc.Capacity()
	if err != nil || c.Used != 1 {
		t.Fatalf("uncertain resource release returned capacity: %+v %v", c, err)
	}
}

func TestSharedCapacityDatabaseFailureIsNotZero(t *testing.T) {
	db := newTestDB(t)
	svc := NewSandboxService(db, nil, nil, nil, SandboxOptions{Max: 2})
	if err := db.Migrator().DropTable(&models.Sandbox{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Capacity(); err == nil {
		t.Fatal("failed inventory reported empty")
	}
	if err := svc.createSandboxRow(&models.Sandbox{Name: "blocked", Status: "creating"}); err == nil {
		t.Fatal("admitted without inventory")
	}
}

func TestSharedCapacityHandoffDoesNotPublishRunning(t *testing.T) {
	db := newTestDB(t)
	svc := NewSandboxService(db, nil, nil, nil, SandboxOptions{Max: 1})
	row := models.Sandbox{Name: "incomplete-restore", Status: "stopped", Purpose: SandboxPurposeSolo}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	release, err := svc.reserveSandbox(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	// External provisioning has started, but the verified recovery state has
	// not been committed. Neither completion nor a repeated rollback may
	// announce readiness or allow another workload to take this slot.
	release(true)
	release(false)
	if err := db.First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != "resuming" {
		t.Fatalf("unfinished recovery was published as %q", row.Status)
	}
	c, err := svc.Capacity()
	if err != nil || c.Used != 1 {
		t.Fatalf("unfinished recovery returned capacity: %+v %v", c, err)
	}
	var limit ErrSandboxLimit
	err = svc.createSandboxRow(&models.Sandbox{Name: "blocked", Status: "creating", Purpose: "run"})
	if !errors.As(err, &limit) {
		t.Fatalf("admitted while recovery is unconfirmed: %v", err)
	}
	if !errors.Is(err, runtime.ErrSandboxCapacity) {
		t.Fatalf("capacity rejection does not match the runtime sentinel: %v", err)
	}
}
