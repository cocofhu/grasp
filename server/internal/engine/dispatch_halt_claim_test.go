package engine

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"gorm.io/gorm"
)

// Regression (flaky TestPauseStillPendingRespectsResolvedGate): a dispatcher
// tick that passed the halt check before Halt must not claim a run queued
// after Halt returned. The tick is parked inside its queue read; Halt runs;
// the run is queued once Halt has returned; then the tick is released.
func TestDispatcherDoesNotClaimAfterHaltReturns(t *testing.T) {
	// Register the callback before the dispatcher goroutine exists: gorm
	// callback registration is not safe against concurrent queries.
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	var armed atomic.Bool
	entered := make(chan struct{})
	release := make(chan struct{})
	if err := db.Callback().Query().Before("gorm:query").Register("test:park-claim", func(tx *gorm.DB) {
		if tx.Statement.Table != "runs" || !strings.Contains(fmt.Sprint(tx.Statement.Clauses["WHERE"]), "queued") {
			return
		}
		if armed.CompareAndSwap(true, false) {
			close(entered)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	arts := services.NewArtifactService(db)
	host := mcp.NewHost(arts)
	eng := New(db, &fakeProvider{host: host}, host, arts, 5)
	eng.SetBlobStore(blob.NewMemory())
	cleanupEngineDB(t, eng, db)

	runID := "halt-claim-" + t.Name()
	ch, unsub := eng.broker.Subscribe(runID)
	defer unsub()

	armed.Store(true)
	eng.signalDispatch()
	select {
	case <-entered:
	case <-time.After(waitPollTimeout):
		t.Fatal("dispatcher never read the queue")
	}

	haltDone := make(chan struct{})
	go func() { eng.Halt(); close(haltDone) }()
	// Wait until Halt has either returned or is blocked behind the parked
	// tick (a pending writer makes TryRLock fail).
	for {
		select {
		case <-haltDone:
		default:
			if eng.haltMu.TryRLock() {
				eng.haltMu.RUnlock()
				time.Sleep(time.Millisecond)
				continue
			}
		}
		break
	}
	queue := func() {
		if err := db.Create(&models.Run{ID: runID, WorkflowID: "w", WorkflowName: "w", Status: "queued",
			Graph: testClarifyGraph()}).Error; err != nil {
			t.Fatalf("create run: %v", err)
		}
	}
	select {
	case <-haltDone:
		queue() // Halt already returned: the parked tick must not claim this run.
		close(release)
	default:
		close(release) // Halt is waiting for the tick; queue once it returns.
		<-haltDone
		queue()
	}

	deadline := time.Now().Add(waitPollTimeout)
	for eng.ActiveDrivers() > 0 {
		if time.Now().After(deadline) {
			t.Fatal("dispatcher slot never released")
		}
		time.Sleep(time.Millisecond)
	}
	for {
		select {
		case raw := <-ch:
			var f struct{ Type, Status string }
			_ = json.Unmarshal(raw, &f)
			if f.Type == "status" && f.Status == "running" {
				t.Fatal("dispatcher admitted a run queued after Halt returned")
			}
			continue
		default:
		}
		break
	}
	var got models.Run
	if err := db.First(&got, "id = ?", runID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != "queued" {
		t.Fatalf("status = %q, want queued", got.Status)
	}
}
