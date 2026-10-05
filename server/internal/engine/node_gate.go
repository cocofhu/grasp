package engine

import (
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	gatenode "github.com/cocofhu/grasp/internal/models/nodereg"
	"github.com/cocofhu/grasp/internal/runtime"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/rs/zerolog/log"
)

func (e *Engine) execGate(c *execCtx, node *models.Node) nodeOutcome {

	iter := c.iter[node.ID]
	var gate models.Gate
	err := e.db.Where("run_id = ? AND node_id = ? AND iteration = ?", c.run.ID, node.ID, iter).First(&gate).Error
	if err == nil && gate.Resolved {
		return nodeOutcome{status: "completed", outputMd: "审批已完成", outputs: map[string]any{"resolved": true}}
	}
	if err != nil {
		upNodeID, upIter := e.resolveGateUpstreamPointer(c, node)
		gate = models.Gate{RunID: c.run.ID, NodeID: node.ID, Iteration: iter, WorkflowID: c.run.WorkflowID, WorkflowName: c.run.WorkflowName,
			Title:             str(node.Config["title"]),
			BodyMd:            e.interpolate(c, str(node.Config["body_template"])),
			Actions:           parseActions(node.Config["actions"]),
			Form:              parseForm(node.Config["form"]),
			UpstreamNodeID:    upNodeID,
			UpstreamIteration: upIter,
			RequestedAt:       time.Now()}
		logDB(e.db.Create(&gate), c.run.ID, "create gate")
		if e.shareRevoker != nil {
			e.shareRevoker.RevokeUnusedForNode(c.run.ID, node.ID)
		}
	}
	return nodeOutcome{status: "paused", outputMd: "等待人工门禁审批…"}
}

// resolveGateUpstreamPointer picks the main upstream node (page preferred) and
// the latest completed iteration for that node at gate-create time. Returns
// empty values when the template has no upstream refs or no iteration is known.
func (e *Engine) resolveGateUpstreamPointer(c *execCtx, node *models.Node) (string, int) {
	upNodeID := gatenode.GatePrimaryUpstreamNodeID(node)
	if upNodeID == "" {
		return "", 0
	}
	var sr models.StateRun
	err := e.db.Where("run_id = ? AND node_id = ? AND status = ?", c.run.ID, upNodeID, "completed").
		Order("iteration desc").First(&sr).Error
	if err == nil && sr.Iteration > 0 {
		return upNodeID, sr.Iteration
	}
	if it := c.iter[upNodeID]; it > 0 {
		return upNodeID, it
	}
	return "", 0
}

// captureDeliverable persists an Agent's primary textual output as a run
// artifact when it has not already written one via the artifact-store MCP, so
// an Agent that declares no products is still reflected in the run's artifacts.
func (e *Engine) captureDeliverable(c *execCtx, node *models.Node, res runtime.NodeResult) {
	content := pickDeliverable(res)
	if strings.TrimSpace(content) == "" {
		return
	}
	for _, a := range e.store.List(c.run.ID) {
		// Feedback ledger products hang off the real node id so the UI can group
		// them, but they are platform-written side records — treating one as
		// "this node already produced something" would suppress the node's
		// actual deliverable the moment a reviewer pushes back.
		if a.Node == node.ID && a.Name != mcp.NodeOutcomeArtifactName &&
			!services.IsFeedbackArtifactName(a.Name) {
			return
		}
	}
	if _, err := e.store.Save(c.run.ID, node.ID, node.ID+".md", "markdown", content); err != nil {
		log.Warn().Err(err).Str("node", node.ID).Msg("auto-capture deliverable failed")
	}
}

// pickDeliverable selects the node's primary human-facing output.
func pickDeliverable(res runtime.NodeResult) string {
	for _, k := range []string{"clarified_requirement", "content"} {
		if s, ok := res.Outputs[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return res.OutputMd
}
