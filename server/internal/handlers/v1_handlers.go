package handlers

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/auth/apikey"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

// --- API Key management (/api/*, session auth) ------------------------------

type createAPIKeyBody struct {
	Name string `json:"name"`
}

func (h *Handlers) ListAPIKeys(c *gin.Context) {
	wfID := c.Param("id")
	if _, ok := h.WF.Get(wfID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	keys := h.APIKeys.List(wfID)
	out := make([]gin.H, 0, len(keys))
	for _, k := range keys {
		out = append(out, apiKeyDTO(k))
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) CreateAPIKey(c *gin.Context) {
	wfID := c.Param("id")
	if _, ok := h.WF.Get(wfID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var b createAPIKeyBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	res, err := h.APIKeys.Create(wfID, b.Name)
	if err != nil {
		if errors.Is(err, services.ErrWorkflowNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"id":         res.Key.ID,
		"name":       res.Key.Name,
		"key":        res.Plaintext,
		"key_prefix": res.Key.KeyPrefix,
		"created_at": res.Key.CreatedAt,
	})
}

func (h *Handlers) RevokeAPIKey(c *gin.Context) {
	wfID := c.Param("id")
	keyID := c.Param("keyId")
	if !h.APIKeys.Revoke(wfID, keyID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

// --- /v1 external API (Bearer API Key auth) --------------------------------

type v1StartRunBody struct {
	Inputs  map[string]any    `json:"inputs"`
	Trigger string            `json:"trigger"` // optional; empty → api; must be manual|api|pm_mcp
	Tags    []string          `json:"tags"`
	Env     []models.EnvEntry `json:"env"` // optional run-scoped sandbox env snapshot
}

func (h *Handlers) V1StartRun(c *gin.Context) {
	wfID, ok := apikey.WorkflowID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	pathID := c.Param("id")
	if pathID != wfID {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var b v1StartRunBody
	if err := c.ShouldBindJSON(&b); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}
	tags, err := models.NormalizeRunTags(b.Tags)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	run, err := h.Eng.StartRunFromPublished(wfID, b.Inputs, b.Trigger, tags, b.Env)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "not published") {
			c.JSON(http.StatusBadRequest, gin.H{"error": msg})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return
	}
	if h.WF != nil {
		if _, ok := h.WF.Get(wfID); ok {
			trigger := run.Trigger
			if trigger == "" {
				trigger = b.Trigger
			}
			payload := map[string]any{
				"workflowId": wfID,
				"trigger":    trigger,
				"source":     "v1_api",
				"runId":      run.ID,
			}
			if len(run.SandboxEnv) > 0 {
				payload["env"] = services.MaskSandboxEnvForAudit(run.SandboxEnv)
			}
			h.recordAudit(services.AuditRecord{
				Actor:        services.SystemActor(), // V1 API Key path has no Session
				CallerKind:   models.CallerKindAPIKey,
				Action:       models.AuditActionRunStart,
				ResourceType: "run",
				ResourceID:   run.ID,
				RunID:        run.ID,
				Outcome:      models.AuditOutcomeOK,
				Summary:      "start run (v1 api)",
				Payload:      payload,
			})
		}
	}
	c.JSON(http.StatusOK, v1StartRunDTO(*run))
}

func (h *Handlers) V1GetRun(c *gin.Context) {
	wfID, ok := apikey.WorkflowID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	run, ok := h.Runs.Get(c.Param("id"))
	if !ok || run.WorkflowID != wfID {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	nodeIDs := h.Runs.CurrentNodeIDs([]models.Run{run})
	var errMsg string
	if run.Status == "failed" {
		// Prefer the aggregated display reason (covers early-exit fallbacks
		// where StateRun.error was never written).
		errMsg = h.Runs.AggregateRunFailure(run.ID).DisplayReason()
	}
	c.JSON(http.StatusOK, v1RunDTO(run, nodeIDs[run.ID], errMsg))
}

func (h *Handlers) V1RunArtifacts(c *gin.Context) {
	wfID, ok := apikey.WorkflowID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	runID := c.Param("id")
	run, ok := h.Runs.Get(runID)
	if !ok || run.WorkflowID != wfID {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	arts := h.Arts.ByRun(runID)
	out := make([]gin.H, 0, len(arts))
	for _, a := range arts {
		out = append(out, v1ArtifactDTO(a))
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) V1DownloadArtifact(c *gin.Context) {
	wfID, ok := apikey.WorkflowID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	a, ok := h.Arts.GetByID(c.Param("id"))
	if !ok || a.WorkflowID != wfID {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	body, mime := decodeArtifactDownloadBody(a)
	c.Header("Content-Disposition", "attachment; filename="+a.Name)
	c.Data(http.StatusOK, mime, body)
}

func (h *Handlers) V1CancelRun(c *gin.Context) {
	wfID, ok := apikey.WorkflowID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	runID := c.Param("id")
	run, ok := h.Runs.Get(runID)
	if !ok || run.WorkflowID != wfID {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err := h.Eng.Cancel(runID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.recordAudit(services.AuditRecord{
		Actor:        services.SystemActor(), // V1 API Key path has no Session
		CallerKind:   models.CallerKindAPIKey,
		Action:       models.AuditActionRunCancel,
		ResourceType: "run",
		ResourceID:   runID,
		RunID:        runID,
		Outcome:      models.AuditOutcomeOK,
		Summary:      "cancel run (v1 api)",
		Payload:      map[string]any{"workflowId": run.WorkflowID, "source": "v1_api", "runId": runID},
	})
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}
