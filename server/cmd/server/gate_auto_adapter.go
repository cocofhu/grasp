package main

import (
	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/services"
)

// gateAutoEngineAdapter bridges engine.GateAutoInvoker → GateAutoInvokeService
// without creating an engine↔services cycle on the event type.
type gateAutoEngineAdapter struct {
	svc *services.GateAutoInvokeService
}

func (a gateAutoEngineAdapter) NotifyGatePaused(ev engine.GateAutoInvokeEvent) {
	if a.svc == nil {
		return
	}
	a.svc.Enqueue(services.GateAutoTask{
		RunID:       ev.RunID,
		WorkflowID:  ev.WorkflowID,
		NodeID:      ev.NodeID,
		NodeType:    ev.NodeType,
		NodeLabel:   ev.NodeLabel,
		GateID:      ev.GateID,
		GateTitle:   ev.GateTitle,
		GateBodyMd:  ev.GateBodyMd,
		GateActions: ev.GateActions,
		Vars:        ev.Vars,
		PathSummary: ev.PathSummary,
	})
}
