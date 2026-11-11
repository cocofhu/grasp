package gateshare

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

// TestTakeoverSessionShareableLikeReview (S5): a parked takeover of a plain
// auto Agent passes the same share-link gate as a review node; once the
// visit ends it is not shareable any more.
func TestTakeoverSessionShareableLikeReview(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "share.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	g := models.Graph{Nodes: []models.Node{{ID: "impl", Type: "agent", Caps: &models.AgentCapabilities{}}}}
	db.Create(&models.Run{ID: "run-to", Status: "waiting_human", Graph: g, StartedAt: time.Now()})
	db.Create(&models.StateRun{RunID: "run-to", NodeID: "impl", NodeType: "agent", Iteration: 1,
		Status: "waiting_human", Mode: models.NodeModeManual})
	db.Create(&models.ReactConversation{RunID: "run-to", NodeID: "impl", Iteration: 1, Takeover: true})
	s := NewService(db, nil)
	if _, _, err := s.currentPendingReview("run-to", "impl"); err != nil {
		t.Fatalf("takeover session must be shareable like a review: %v", err)
	}
	db.Model(&models.StateRun{}).Where("run_id = ?", "run-to").Update("mode", models.NodeModeAuto)
	if _, _, err := s.currentPendingReview("run-to", "impl"); err != ErrNotReviewSession {
		t.Fatalf("plain auto agent visit: err=%v, want ErrNotReviewSession", err)
	}
}
