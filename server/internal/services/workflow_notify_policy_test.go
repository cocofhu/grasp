package services

import (
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/models"
)

// TestUpdateNotifyPolicyDoesNotDemotePublished covers review v1 / g1.3:
// notify-only write must keep published status and leave Graph untouched
// even when a stale list DTO would have carried an older graph.
func TestUpdateNotifyPolicyDoesNotDemotePublished(t *testing.T) {
	db := newTestDB(t)
	svc := NewWorkflowService(db)

	wf := models.WorkflowDef{
		ID:               "wf-notify-only",
		ProjectID:        models.DefaultProjectID,
		Name:             "SelfIter",
		Version:          3,
		PublishedVersion: 3,
		Graph: models.Graph{
			Nodes: []models.Node{
				{ID: "in", Type: "input", Label: "Fresh Start"},
				{ID: "g", Type: "human_gate", Label: "Gate"},
				{ID: "out", Type: "output", Label: "End"},
			},
			Edges: []models.Edge{
				{ID: "e1", Source: "in", Target: "g"},
				{ID: "e2", Source: "g", Target: "out"},
			},
		},
		NotifyPolicy: models.WorkflowNotifyPolicy{Mode: models.NotifyModeInherit},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := db.Create(&wf).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := svc.UpdateNotifyPolicy(wf.ID, models.WorkflowNotifyPolicy{
		Mode:   models.NotifyModeCustom,
		Events: []string{models.NotifyKindWaitingHuman, models.NotifyKindFailed},
	})
	if err != nil {
		t.Fatalf("UpdateNotifyPolicy: %v", err)
	}
	if got.Status() != "published" {
		t.Fatalf("status demoted to %q", got.Status())
	}
	if got.Version != 3 {
		t.Fatalf("version mutated to %d", got.Version)
	}
	if got.NotifyPolicy.Mode != models.NotifyModeCustom {
		t.Fatalf("mode=%q", got.NotifyPolicy.Mode)
	}
	if len(got.Graph.Nodes) != 3 || got.Graph.Nodes[0].Label != "Fresh Start" {
		t.Fatalf("graph rewritten: %+v", got.Graph.Nodes)
	}

	reloaded, ok := svc.Get(wf.ID)
	if !ok {
		t.Fatal("missing after update")
	}
	if reloaded.Status() != "published" || reloaded.Graph.Nodes[0].Label != "Fresh Start" {
		t.Fatalf("persisted row corrupted: status=%s label=%s", reloaded.Status(), reloaded.Graph.Nodes[0].Label)
	}
}

func TestFormatRunDeepLinkRelativeWhenBaseEmpty(t *testing.T) {
	if got := FormatRunDeepLinkForTest("", "run-1"); got != "/runs/run-1" {
		t.Fatalf("got %q", got)
	}
	if got := FormatRunDeepLinkForTest("https://app.example/", "run-1"); got != "https://app.example/runs/run-1" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatCompletedRunDeepLink(t *testing.T) {
	got := FormatCompletedRunDeepLinkForTest("https://app.example", "run-1", "output_end")
	want := "https://app.example/runs/run-1?node=output_end&tab=output"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	noNode := FormatCompletedRunDeepLinkForTest("", "run-2", models.NotifyCompletedSentinelNodeID)
	if noNode != "/runs/run-2?tab=output" {
		t.Fatalf("sentinel deep link: %q", noNode)
	}
	emptyNode := FormatCompletedRunDeepLinkForTest("", "run-3", "")
	if emptyNode != "/runs/run-3?tab=output" {
		t.Fatalf("empty node deep link: %q", emptyNode)
	}
}
