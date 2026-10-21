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

// A takeover opening turn whose text is empty but which carries the auto
// phase's narration must still reach public previews; a turn with neither is
// dropped as before.
func TestSanitizeTurnsKeepsNarrationOnlyTurn(t *testing.T) {
	got := SanitizeTurns([]models.ReactMessage{
		{Role: "agent", Text: "", Narration: "我已经完成了登录页的初稿。", At: "2026-10-21T10:00:00+08:00"},
		{Role: "agent", Text: "", Narration: "   "},
		{Role: "human", Text: "", Narration: "人类消息不带叙述"},
	})
	if len(got) != 1 {
		t.Fatalf("turns = %+v, want only the narration-bearing agent turn", got)
	}
	if got[0].Role != "agent" || got[0].Narration != "我已经完成了登录页的初稿。" || got[0].Text != "" {
		t.Fatalf("turn = %+v", got[0])
	}
}
