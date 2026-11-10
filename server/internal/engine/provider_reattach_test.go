package engine

import (
	"encoding/json"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"testing"
)

func dispatchedVisitFixture(t *testing.T) (*Engine, *execCtx, *models.Node, models.StateRun, models.Sandbox) {
	t.Helper()
	e, c, node, state := workspaceCheckpointFixture(t, true)
	req := e.nodeReq(c, node)
	op, err := req.PrepareProviderAttempt(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	resource := models.Sandbox{Name: "exact-provider-resource", RunID: req.RunID, NodeID: req.NodeID, StateRunID: state.ID, Iteration: req.Iteration, RunAttempt: req.RunAttempt, ProviderAttempt: op.Attempt, Purpose: "run", Status: "running", Generation: 7}
	if err := e.db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	op.Resource, op.SessionID, op.Workspace, op.Authorization = resource.Name, "exact-native-session", "/workspace", "current-grant"
	if err := req.BeforeProviderDispatch(t.Context(), op); err != nil {
		t.Fatal(err)
	}
	if err := e.db.First(&state, state.ID).Error; err != nil {
		t.Fatal(err)
	}
	return e, c, node, state, resource
}

func TestProviderReattachReservesExactVisitAndLoadsSameReceiptWithoutNewAttempt(t *testing.T) {
	e, c, node, state, resource := dispatchedVisitFixture(t)
	var receipt sandbox.OperationIdentity
	if err := json.Unmarshal([]byte(state.ProviderOperationReceipt), &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Valid() || receipt.SandboxID != resource.ID || receipt.Generation != 7 {
		t.Fatal("receipt not frozen", receipt)
	}
	// A later row for another visit must never win a "latest resource" lookup.
	wrong := resource
	wrong.ID = 0
	wrong.Name = "newer-foreign-visit"
	wrong.Iteration++
	if err := e.db.Create(&wrong).Error; err != nil {
		t.Fatal(err)
	}
	e.reconcileInterrupted()
	if len(e.pendingReattach) != 1 || e.pendingReattach[0].sandbox != resource.Name || e.pendingReattach[0].iteration != state.Iteration {
		t.Fatal(e.pendingReattach)
	}
	c.reattachSandbox = resource.Name
	req := e.nodeReq(c, node)
	if !req.AttachOnly || req.Operation != receipt || req.ProviderAttempt != state.ProviderAttempt {
		t.Fatal("original receipt lost", req.Operation)
	}
	var after models.StateRun
	if err := e.db.First(&after, state.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.ProviderOperationID != state.ProviderOperationID || after.ProviderAttempt != state.ProviderAttempt || after.WorkspaceCheckpointID != "" {
		t.Fatal("startup replayed/parked verified original", after)
	}
}

func TestProviderReattachRejectsMissingOrChangedOriginalProvenance(t *testing.T) {
	for _, change := range []string{"operation", "receipt", "attempt", "generation", "visit", "resource", "checkpoint", "manual"} {
		t.Run(change, func(t *testing.T) {
			e, c, _, state, resource := dispatchedVisitFixture(t)
			switch change {
			case "operation":
				state.ProviderOperationID = ""
			case "receipt":
				state.ProviderOperationReceipt = "{}"
			case "attempt":
				state.ProviderAttempt++
			case "generation":
				e.db.Table("sandboxes").Where("id = ?", resource.ID).Update("generation", 8)
			case "visit":
				state.Iteration++
			case "resource":
				e.db.Table("sandboxes").Where("id = ?", resource.ID).Update("name", "different-resource")
			case "checkpoint":
				state.WorkspaceCheckpointID = "already-sealed"
			case "manual":
				state.Mode = models.NodeModeManual
			}
			if name, ok := e.exactReattachSandbox(c.run, state); ok {
				t.Fatal("unproven resource adopted", name)
			}
		})
	}
}

func TestProviderDispatchCannotPublishReceiptForCancelledVisit(t *testing.T) {
	e, c, node, state := workspaceCheckpointFixture(t, true)
	req := e.nodeReq(c, node)
	op, err := req.PrepareProviderAttempt(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	resource := models.Sandbox{Name: "cancelled-visit-resource", Purpose: "run", RunID: req.RunID, NodeID: req.NodeID, StateRunID: state.ID, Iteration: req.Iteration, RunAttempt: req.RunAttempt, ProviderAttempt: op.Attempt, Status: "running"}
	if err := e.db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	op.Resource, op.SessionID, op.Workspace, op.Authorization = resource.Name, "native-session", "/workspace", "grant"
	if err := e.db.Table("state_runs").Where("id = ?", state.ID).Update("status", "cancelled").Error; err != nil {
		t.Fatal(err)
	}
	if err := req.BeforeProviderDispatch(t.Context(), op); err == nil {
		t.Fatal("late cancelled visit authorized native dispatch")
	}
	if err := e.db.Take(&state, state.ID).Error; err != nil {
		t.Fatal(err)
	}
	if state.ProviderDispatchState != "preparing" || state.ProviderOperationReceipt != "" {
		t.Fatal("late receipt published", state.ProviderDispatchState)
	}
}
