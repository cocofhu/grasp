package storageguard_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/storageguard"
	"github.com/google/uuid"
)

func TestMaintenanceRejectsLiveOrNonexpiredCrashedRuntime(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "db.sqlite")}
	runtime, err := database.OpenRuntime(cfg, filepath.Join(dir, "payloads"))
	if err != nil {
		t.Fatal(err)
	}
	control, err := database.ConnectWithFiles(cfg, filepath.Join(dir, "control-payloads"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(control) }()
	owner := uuid.NewString()
	if err := storageguard.Enter(ctx, control, owner, nil); err == nil {
		t.Fatal("maintenance admitted a live server")
	}
	// Simulate abrupt death: close its SQL pool without unregistering.
	if err := database.Close(runtime.DB); err != nil {
		t.Fatal(err)
	}
	if err := storageguard.Enter(ctx, control, owner, []string{uuid.NewString()}); err == nil {
		t.Fatal("unrelated or aged presence was treated as stopped")
	}
	if err := storageguard.Enter(ctx, control, owner, []string{runtime.ID}); err != nil {
		t.Fatal(err)
	}
	if other, err := database.OpenRuntime(cfg, filepath.Join(dir, "other-payloads")); err == nil {
		_ = other.Close()
		t.Fatal("new server passed maintenance fence")
	}
	if err := storageguard.Enter(ctx, control, uuid.NewString(), nil); err == nil {
		t.Fatal("maintenance owner was replaced")
	}
	if err := storageguard.Leave(ctx, control, uuid.NewString()); err == nil {
		t.Fatal("foreign operation released maintenance")
	}
	if err := storageguard.Leave(ctx, control, owner); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterAndMaintenanceHaveOneAtomicAdmissionBoundary(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.DatabaseConfig{Driver: "sqlite", Path: filepath.Join(dir, "db.sqlite")}
	a, err := database.ConnectWithFiles(cfg, filepath.Join(dir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(a) }()
	b, err := database.ConnectWithFiles(cfg, filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close(b) }()
	if _, err := storageguard.Inspect(ctx, a); err != nil {
		t.Fatal(err)
	}
	for range 20 {
		owner := uuid.NewString()
		var id string
		var registerErr, maintenanceErr error
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; id, registerErr = storageguard.Register(ctx, a) }()
		go func() { defer wg.Done(); <-start; maintenanceErr = storageguard.Enter(ctx, b, owner, nil) }()
		close(start)
		wg.Wait()
		if (registerErr == nil) == (maintenanceErr == nil) {
			t.Fatalf("one admission must win: register=%v maintenance=%v", registerErr, maintenanceErr)
		}
		if registerErr == nil {
			if err := storageguard.Unregister(ctx, a, id); err != nil {
				t.Fatal(err)
			}
		} else if err := storageguard.Leave(ctx, b, owner); err != nil {
			t.Fatal(err)
		}
	}
}
