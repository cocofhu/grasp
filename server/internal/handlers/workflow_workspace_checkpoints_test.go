package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type checkpointAPIProbe struct {
	row                        models.WorkflowVisitCheckpoint
	err                        error
	actor, runID, checkpointID string
	reads, reconciles          int
}

func (p *checkpointAPIProbe) WorkspaceCheckpoints(_ context.Context, actor, runID string) ([]models.WorkflowVisitCheckpoint, error) {
	p.actor, p.runID, p.reads = actor, runID, p.reads+1
	return []models.WorkflowVisitCheckpoint{p.row}, p.err
}

func (p *checkpointAPIProbe) ReconcileWorkspaceCheckpoint(_ context.Context, actor, runID, id string) (models.WorkflowVisitCheckpoint, error) {
	p.actor, p.runID, p.checkpointID, p.reconciles = actor, runID, id, p.reconciles+1
	return p.row, p.err
}

func TestWorkflowCheckpointAPIUsesCurrentActorOriginalScopeAndSafeMetadata(t *testing.T) {
	h, _, _ := restoreAPIFixture(t)
	probe := &checkpointAPIProbe{row: models.WorkflowVisitCheckpoint{ID: "checkpoint-original", RunID: "run-original", SnapshotID: "snapshot-original", FileState: "unknown", RouteState: "sealed", Reason: "snapshot_unknown", ActorID: "private-actor", Resource: "private-resource", OutcomeRef: "private-object-reference", BindingFingerprint: "private-binding"}}
	h.WorkspaceCheckpoints = probe
	c, w := restoreAPIContext(http.MethodGet, "", false)
	c.Params = gin.Params{{Key: "id", Value: "run-original"}}
	h.ListWorkflowWorkspaceCheckpoints(c)
	if w.Code != http.StatusOK || probe.reads != 1 || probe.reconciles != 0 || probe.actor != "alice" || probe.runID != "run-original" || strings.Contains(w.Body.String(), "private") || !strings.Contains(w.Body.String(), `"reason":"snapshot_unknown"`) {
		t.Fatal("metadata read exposed internals or mutated checkpoint", w.Code, w.Body, probe)
	}
	c, w = restoreAPIContext(http.MethodPost, `{"actorId":"bob"}`, false)
	c.Params = gin.Params{{Key: "id", Value: "run-original"}, {Key: "checkpointId", Value: "checkpoint-original"}}
	h.ReconcileWorkflowWorkspaceCheckpoint(c)
	if w.Code != http.StatusOK || probe.reconciles != 1 || probe.actor != "alice" || probe.checkpointID != "checkpoint-original" {
		t.Fatal("reconcile did not target authenticated actor and original receipt", w.Code, probe)
	}
	c, w = restoreAPIContext(http.MethodPost, "", true)
	h.ReconcileWorkflowWorkspaceCheckpoint(c)
	if w.Code != http.StatusForbidden || probe.reconciles != 1 {
		t.Fatal("embedded chat received workflow checkpoint authority", w.Code, probe)
	}
	probe.row.Reason = "private arbitrary upstream diagnostic"
	c, w = restoreAPIContext(http.MethodGet, "", false)
	h.ListWorkflowWorkspaceCheckpoints(c)
	if strings.Contains(w.Body.String(), "private") {
		t.Fatal("unknown reason escaped the response whitelist", w.Body)
	}
}

func TestWorkflowCheckpointAPIErrorsDoNotDisclosePrivateDiagnostics(t *testing.T) {
	h, _, _ := restoreAPIFixture(t)
	probe := &checkpointAPIProbe{}
	h.WorkspaceCheckpoints = probe
	for _, item := range []struct {
		err    error
		status int
	}{{gorm.ErrRecordNotFound, 404}, {services.ErrWorkspaceJobUnsupported, 501}, {services.ErrWorkflowCheckpointBlocked, 409}, {errors.New("private endpoint diagnostic"), 503}} {
		probe.err = item.err
		c, w := restoreAPIContext(http.MethodGet, "", false)
		h.ListWorkflowWorkspaceCheckpoints(c)
		if w.Code != item.status || strings.Contains(w.Body.String(), "private") {
			t.Fatal(w.Code, w.Body)
		}
	}
}
