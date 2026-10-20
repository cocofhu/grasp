package services

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

func TestRunServiceListByTagsANDExactMatch(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	base := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)

	db.Create(&models.Run{ID: "run-both", Status: "running", WorkflowID: "wf-1", Tags: []string{"bugfix", "spike"}, StartedAt: base.Add(3 * time.Second), CreatedAt: base})
	db.Create(&models.Run{ID: "run-one", Status: "running", WorkflowID: "wf-1", Tags: []string{"bugfix"}, StartedAt: base.Add(2 * time.Second), CreatedAt: base})
	db.Create(&models.Run{ID: "run-substring", Status: "running", WorkflowID: "wf-1", Tags: []string{"bugfix"}, StartedAt: base.Add(time.Second), CreatedAt: base})

	got := s.ListByTags(nil, "", []string{"bugfix", "spike"})
	if len(got) != 1 || got[0].ID != "run-both" {
		t.Fatalf("AND tags result = %v, want [run-both]", idsOf(got))
	}

	got = s.ListByTags(nil, "", []string{"bug"})
	if len(got) != 0 {
		t.Fatalf("substring tag should not match: %v", idsOf(got))
	}
}

func TestWorkspaceRunTags(t *testing.T) {
	db := newTestDB(t)
	s := NewRunService(db)
	db.Create(&models.WorkflowDef{ID: "wf-1", Name: "WF 1", Graph: validGraph()})
	db.Create(&models.WorkflowDef{ID: "wf-2", Name: "WF 2", Graph: validGraph()})
	db.Create(&models.Run{ID: "r1", WorkflowID: "wf-1", Status: "completed", Tags: []string{"bugfix", "spike"}})
	db.Create(&models.Run{ID: "r2", WorkflowID: "wf-2", Status: "completed", Tags: []string{"release", "bugfix"}})

	got := s.RunTags()
	want := []string{"bugfix", "release", "spike"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d = %q want %q", i, got[i], want[i])
		}
	}
}
