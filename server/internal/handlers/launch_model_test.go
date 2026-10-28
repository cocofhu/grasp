package handlers_test

import (
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestStartRunRejectsLaunchModelWithoutClarify(t *testing.T) {
	h := newHarness(t)
	if err := h.h.WF.Save(&models.WorkflowDef{
		ID: "wf-plain", Name: "plain",
		Graph: models.Graph{
			Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}},
			Edges: []models.Edge{{ID: "e1", Source: "in", Target: "out"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	w := h.do(http.MethodPost, "/api/workflows/wf-plain/runs", map[string]any{
		"trigger":               "manual",
		"launchConfigurationId": "group",
		"launchModelId":         "",
		"launchReasoningEffort": "high",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var n int64
	if err := h.db.Model(&models.Run{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("illegal launch created %d runs", n)
	}
}
