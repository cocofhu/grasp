package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestCapacityEndpointCountsAllPurposesAndUncertainCleanup(t *testing.T) {
	h := newHarness(t)
	for i, status := range []string{"creating", "running", "saving", "error", "stopped"} {
		row := models.Sandbox{Name: status, Status: status, Purpose: []string{"test", "solo", "run", "agent", "solo"}[i]}
		if err := h.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	w := h.do(http.MethodGet, "/api/sandboxes/capacity", nil)
	var result services.SandboxCapacity
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || result.Used != 4 || result.Limit != 2 {
		t.Fatalf("unexpected shared capacity: %d %s", w.Code, w.Body)
	}
	if err := h.db.Migrator().DropTable(&models.Sandbox{}); err != nil {
		t.Fatal(err)
	}
	if w := h.do(http.MethodGet, "/api/sandboxes/capacity", nil); w.Code != http.StatusInternalServerError {
		t.Fatalf("failed inventory reported success: %d %s", w.Code, w.Body)
	}
}

func TestSettingsRejectsRetiredPolicyKeysAtomically(t *testing.T) {
	h := newHarness(t)
	h.h.Settings = services.NewSettingsService(h.db, h.h.Eng, h.h.Sbx)
	for _, key := range []string{"max_test_sandboxes", "max_solo_sandboxes", "run_sandbox_ttl_minutes", "run_sandbox_max_retention_hours", "test_sandbox_ttl_minutes"} {
		w := h.do(http.MethodPut, "/api/settings", map[string]int{key: 20, services.KeyMaxSandboxes: 30})
		if w.Code != http.StatusBadRequest {
			t.Fatalf("retired %s accepted: %d", key, w.Code)
		}
		if h.h.Sbx.MaxSandboxes() != 2 {
			t.Fatal("invalid mixed patch partially applied")
		}
	}
	w := h.do(http.MethodPut, "/api/settings", map[string]int{services.KeySandboxIdleTTLMin: 90, services.KeyMaxSandboxes: 30})
	if w.Code != http.StatusOK || h.h.Sbx.MaxSandboxes() != 30 {
		t.Fatalf("unified policy rejected: %d %s", w.Code, w.Body)
	}
}
