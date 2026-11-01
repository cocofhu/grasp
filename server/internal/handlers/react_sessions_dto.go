package handlers

import (
	"github.com/cocofhu/grasp/internal/chatsession"
	"github.com/cocofhu/grasp/internal/engine"

	"github.com/gin-gonic/gin"
)

// Capture the generation floor before reading the map. A session created
// during collection may be absent from it and must still be accepted live.
func projectReviewSessionsDTO(out gin.H, eng *engine.Engine, runID string) {
	out["reviewSessionGeneration"] = chatsession.CurrentSessionGeneration()
	if byNode := reactSessionsDTO(eng.ReviewSessionsForRun(runID)); byNode != nil {
		out["reactSessions"] = byNode
	}
	// The same versioned state the run WebSocket sends as type:"dialogue".
	out["dialogueByNode"] = eng.DialoguesForRun(runID)
}

// reactSessionsDTO maps engine review/clarify session snapshots to the
// per-node gin.H used by runDetailDTO and clarify inbox-context (refresh-resume).
func reactSessionsDTO(snaps []engine.ReviewSessionSnapshot) gin.H {
	if len(snaps) == 0 {
		return nil
	}
	byNode := gin.H{}
	for _, s := range snaps {
		row := gin.H{
			"sessionGeneration": s.SessionGeneration, "revision": s.Revision,
			"kind": s.Kind, "waiting": s.Waiting, "busy": s.Busy,
			"items": s.Items, "activeItem": s.ActiveItem,
		}
		// Same keys as the live queue_state frame, including explicit false,
		// so a refresh does not treat a missing field as "bridge idle".
		if s.SandboxKnown {
			row["sandboxBusy"] = s.SandboxBusy
			row["sandboxDesynced"] = s.SandboxDesynced
			if s.SandboxWaiting != 0 {
				row["sandboxWaiting"] = s.SandboxWaiting
			}
			if s.SandboxRunningOpID != "" {
				row["sandboxRunningOpId"] = s.SandboxRunningOpID
			}
		}
		byNode[s.NodeID] = row
	}
	return byNode
}
