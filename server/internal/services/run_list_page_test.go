package services

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
)

func TestRunListPage(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewRunService(db)
	now := time.Now()
	for i, id := range []string{"r1", "r2", "r3"} {
		if err := db.Create(&models.Run{
			ID: id, WorkflowID: "wf", Status: "completed", StartedAt: now.Add(time.Duration(i) * time.Second),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	page, total, err := s.ListPage(nil, "wf", "", 1, 2)
	if err != nil || total != 3 || len(page) != 2 {
		t.Fatalf("page=%d total=%d err=%v", len(page), total, err)
	}
	page2, total2, err := s.ListPage(nil, "wf", "", 2, 2)
	if err != nil || total2 != 3 || len(page2) != 1 {
		t.Fatalf("page2=%d total2=%d err=%v", len(page2), total2, err)
	}
	if err := s.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

// TestRunListPageReportsDBError: an unreadable database surfaces as an error
// (and a failing Ping) instead of an empty page.
func TestRunListPageReportsDBError(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	s := NewRunService(db)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ListPage(nil, "", "", 1, 20); err == nil {
		t.Fatal("expected error from closed database")
	}
	if err := s.Ping(context.Background()); err == nil {
		t.Fatal("expected ping error from closed database")
	}
}

// Demo s4: claim remaining-human_gate tie-break must not alter list ORDER BY.
func TestRunListOrderIndependentOfClaimHumanGate(t *testing.T) {
	got := runListOrderBy("", "")
	if got != defaultRunListOrder {
		t.Fatalf("default list order = %q, want %q", got, defaultRunListOrder)
	}
	if !strings.Contains(defaultRunListOrder, "created_at") {
		t.Fatalf("expected created_at in list order: %q", defaultRunListOrder)
	}
	if strings.Contains(strings.ToLower(defaultRunListOrder), "human_gate") {
		t.Fatalf("list order must not include claim human_gate key: %q", defaultRunListOrder)
	}
	// Whitelist still only started_at|priority — no gate secondary key.
	if got := runListOrderBy("priority", "desc"); got != "priority DESC, id DESC" {
		t.Fatalf("priority sort = %q", got)
	}
}
