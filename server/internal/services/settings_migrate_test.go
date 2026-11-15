package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/config"
	"github.com/cocofhu/grasp/internal/models"
)

func TestSettingsConsumesLegacyCapacityOnce(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "migrate", true: "explicit wins"}[explicit], func(t *testing.T) {
			db := newTestDB(t)
			config.StoreConfig(&config.Config{Sandbox: config.SandboxConfig{IdleTTLMinutes: 60, MaxSandboxes: 15}})
			svc := NewSettingsService(db, nil, nil)
			for key, value := range map[string]int{"max_test_sandboxes": 10, "max_solo_sandboxes": 20, "run_sandbox_ttl_minutes": 30, "test_sandbox_ttl_minutes": 15, "run_sandbox_max_retention_hours": 24} {
				if err := svc.setInt(key, value); err != nil {
					t.Fatal(err)
				}
			}
			if explicit {
				if err := svc.setInt(KeyMaxSandboxes, 7); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err := svc.ApplyOnBoot(); err != nil {
					t.Fatal(err)
				}
			}
			want := 20
			if explicit {
				want = 7
			}
			if n, ok := svc.dbInt(KeyMaxSandboxes); !ok || n != want {
				t.Fatalf("limit=%d exists=%v want=%d", n, ok, want)
			}
			var rows []models.Setting
			if err := db.Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if len(rows) != 1 || rows[0].Key != KeyMaxSandboxes {
				t.Fatalf("legacy keys survived: %+v", rows)
			}
			for _, item := range svc.Effective() {
				if item.Key == KeySandboxIdleTTLMin && item.Value != 60 {
					t.Fatalf("old destructive TTL carried over: %+v", item)
				}
			}
		})
	}
}

func TestSettingsRejectsRemovedKnobsAtomically(t *testing.T) {
	db := newTestDB(t)
	svc := NewSettingsService(db, nil, nil)
	if _, err := svc.UpdateWithBrand(map[string]int{KeyMaxSandboxes: 20, "max_solo_sandboxes": 8}, BrandPatch{}); err == nil {
		t.Fatal("removed knob accepted")
	}
	if _, ok := svc.dbInt(KeyMaxSandboxes); ok {
		t.Fatal("partial patch persisted")
	}
}
