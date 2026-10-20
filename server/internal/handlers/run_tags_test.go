package handlers_test

import (
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
)

func TestStartRunRejectsInvalidTags(t *testing.T) {
	h := newHarness(t)
	if err := h.h.WF.Save(&models.WorkflowDef{
		ID: "wf-tags", Name: "WF Tags",
		Graph: models.Graph{
			Nodes: []models.Node{{ID: "in", Type: "input"}, {ID: "out", Type: "output"}},
			Edges: []models.Edge{{ID: "e1", Source: "in", Target: "out"}},
		},
	}); err != nil {
		t.Fatalf("save workflow: %v", err)
	}

	w := h.do(http.MethodPost, "/api/workflows/wf-tags/runs", map[string]any{
		"tags": []string{"bad tag"},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
