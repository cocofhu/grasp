package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
)

// A sandbox whose gateway probes hang (guest out of memory, relay timing out)
// must not keep the saved session from loading.
func TestSoloGetReturnsSavedSessionWhenLiveProbesHang(t *testing.T) {
	solo, sbx, _, db := newSoloFixture(t, 0, 3)
	var probes, cancelled atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		select {
		case <-r.Context().Done():
			cancelled.Add(1)
		case <-time.After(30 * time.Second):
		}
	}))
	defer gateway.Close()
	sbx.mgr = sandbox.NewManager(sandbox.NewGatewayClient(gateway.URL, ""), sandbox.ManagerOptions{})
	budget := soloLiveViewBudget
	soloLiveViewBudget = 100 * time.Millisecond
	defer func() { soloLiveViewBudget = budget }()

	box := models.Sandbox{Name: "overloaded", Status: "running", Purpose: SandboxPurposeSolo, CodeServerPort: 8744, ThreadID: "overloaded"}
	if err := db.Create(&box).Error; err != nil {
		t.Fatal(err)
	}
	row := models.SoloSession{ID: "overloaded", UserID: "alice", AgentName: "agentA", Status: models.SoloStatusRunning, SandboxID: box.ID}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	view, err := solo.Get(context.Background(), row.ID, "alice")
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("detail waited %s for a hanging sandbox", elapsed)
	}
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != models.SoloStatusRunning || view.Sandbox == nil {
		t.Fatalf("saved session missing: %+v", view)
	}
	if view.Sandbox.Status != "running" || view.Sandbox.ContainerStatus != "unknown" || !view.Sandbox.HasCodeServer {
		t.Fatalf("an unanswered probe must leave saved state and an unknown live status: %+v", view.Sandbox)
	}
	if probes.Load() == 0 {
		t.Fatal("live status was never probed")
	}
	waitFor(t, "abandoned probes cancelled", func() bool { return cancelled.Load() == probes.Load() })
}
