package handlers

import (
	"net/http"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

func (h *Handlers) ListSoloWorkflows(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	runs, err := h.Solo.WorkflowRuns(c.Param("id"), user)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	cards := make([]gin.H, 0, len(runs))
	for _, run := range runs {
		card := runSummaryDTO(run, "")
		nodes := make([]gin.H, 0, len(run.Graph.Nodes))
		states := h.Runs.TaskStates(run.ID)
		for _, node := range run.Graph.Nodes {
			if node.Type == "input" || node.Type == "output" {
				continue
			}
			status := "pending"
			iteration := 0
			for _, state := range states {
				if state.NodeID == node.ID {
					status, iteration = state.Status, state.Iteration
				}
			}
			nodes = append(nodes, gin.H{"id": node.ID, "label": node.Label, "type": node.Type, "status": status, "iteration": iteration})
		}
		card["nodes"] = nodes
		if gate, ok := h.Runs.PendingGate(run.ID); ok {
			card["pendingGate"] = gate
		}
		cards = append(cards, card)
	}
	feature, err := services.WorkflowFeatureFor(h.Runs.DB())
	if err != nil {
		writeAdmissionErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": cards, "workflows": feature})
}

// This route is cookie/session-authenticated and intentionally absent from
// Solo's MCP tools and bearer embed API. A launch card submits it explicitly.
func (h *Handlers) LaunchSoloWorkflow(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	if _, err := h.Solo.Require(c.Param("id"), user); err != nil {
		writeSoloErr(c, err)
		return
	}
	var body struct {
		startRunBody
		Confirmed bool `json:"confirmed"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid launch configuration"})
		return
	}
	if body.ExpectedWorkflowVersion < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请重新选择已发布的工作流", "code": "workflow_version_required"})
		return
	}
	tags, err := models.NormalizeRunTags(body.Tags)
	if err != nil {
		writeAdmissionErr(c, err)
		return
	}
	choice := launchChoiceFromBody(body.startRunBody)
	run, err := h.admission().Start(services.RunStart{WorkflowID: c.Param("workflowId"), Source: "solo", ActorID: user, SoloID: c.Param("id"), Confirmed: body.Confirmed,
		IdempotencyKey: c.GetHeader("Idempotency-Key"), Published: true, Inputs: body.Inputs, Trigger: models.TriggerManual, Priority: body.Priority, Tags: tags, Env: body.Env, Title: body.Title,
		ExpectedVersion:   body.ExpectedWorkflowVersion,
		FirstMessageInput: body.FirstMessage,
		PrepareFirstMessage: func() (*models.CompositeText, error) {
			return h.firstMessage(c.Request.Context(), user, c.Param("workflowId"), body.FirstMessage)
		},
		ModelSet: choice.Set, ConfigurationID: choice.ConfigurationID, ModelID: choice.ModelID, ReasoningEffort: choice.ReasoningEffort})
	if err != nil {
		writeAdmissionErr(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{Actor: h.auditActorFromContext(c), Action: models.AuditActionRunStart, ResourceType: "run", ResourceID: run.ID, RunID: run.ID, Outcome: models.AuditOutcomeOK, Summary: "start workflow from Solo", Payload: map[string]any{"source": "solo", "soloId": c.Param("id"), "workflowId": run.WorkflowID}})
	c.JSON(http.StatusOK, gin.H{"id": run.ID, "status": run.Status})
}
