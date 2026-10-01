package handlers

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

type startRunBody struct {
	Inputs   map[string]any    `json:"inputs"`
	Trigger  string            `json:"trigger"`
	Priority string            `json:"priority"` // high|normal|low; empty → normal
	Tags     []string          `json:"tags"`
	Env      []models.EnvEntry `json:"env"`   // optional run-scoped sandbox env snapshot
	Title    string            `json:"title"` // optional; overrides computeRunTitle when non-blank
	// FirstMessage is the opening chat message (text + attachments) for an
	// approve-first pipeline. The engine delivers it into the approve node's
	// sandbox once that node parks, so the caller can navigate away at once.
	FirstMessage *models.CompositeText `json:"firstMessage"`
}

func (h *Handlers) StartRun(c *gin.Context) {
	var b startRunBody
	if err := c.ShouldBindJSON(&b); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}
	trigger, err := models.ResolveTrigger(b.Trigger, models.TriggerManual)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tags, err := models.NormalizeRunTags(b.Tags)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	run, err := h.Eng.StartRunWithFirstMessage(c.Param("id"), b.Inputs, trigger, b.Priority, tags, b.Env, b.Title, b.FirstMessage)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if wf, ok := h.WF.Get(c.Param("id")); ok && wf.ProjectID != "" {
		payload := map[string]any{
			"workflowId": c.Param("id"),
			"trigger":    trigger,
			"priority":   models.PriorityLabel(run.Priority),
			"runId":      run.ID,
		}
		if len(run.SandboxEnv) > 0 {
			payload["env"] = services.MaskSandboxEnvForAudit(run.SandboxEnv)
		}
		h.recordAudit(services.AuditRecord{
			ProjectID:    wf.ProjectID,
			Actor:        h.auditActorFromContext(c),
			Action:       models.AuditActionRunStart,
			ResourceType: "run",
			ResourceID:   run.ID,
			RunID:        run.ID,
			Outcome:      models.AuditOutcomeOK,
			Summary:      "start run",
			Payload:      payload,
		})
	}
	c.JSON(http.StatusOK, gin.H{"id": run.ID, "status": run.Status, "priority": models.PriorityLabel(run.Priority)})
}

var validRunStatuses = map[string]bool{
	"running":       true,
	"waiting_human": true,
	"queued":        true,
	"completed":     true,
	"failed":        true,
	"cancelled":     true,
}

func parseRunStatuses(raw string) []string {
	if raw == "" {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, part := range strings.Split(raw, ",") {
		s := strings.TrimSpace(part)
		if s != "" && validRunStatuses[s] && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseRunTags(values ...string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			tag, err := models.NormalizeRunTags([]string{part})
			if err != nil || len(tag) == 0 {
				continue
			}
			if _, ok := seen[tag[0]]; ok {
				continue
			}
			seen[tag[0]] = struct{}{}
			out = append(out, tag[0])
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (h *Handlers) ListRuns(c *gin.Context) {
	statuses := parseRunStatuses(c.Query("status"))
	tags := parseRunTags(append(c.QueryArray("tag"), c.Query("tag"))...)
	wf := c.Query("wf")
	projectID := c.Query("projectId")
	sort, order := parseRunListSort(c.Query("sort"), c.Query("order"))
	pg, ok := parsePagination(c)
	if !ok {
		return
	}
	if !pg.Active {
		runs := h.Runs.ListByTags(statuses, wf, projectID, tags, sort, order)
		labels := h.Runs.CurrentNodeLabels(runs)
		out := make([]gin.H, 0, len(runs))
		for _, r := range runs {
			out = append(out, runSummaryDTO(r, labels[r.ID]))
		}
		c.JSON(http.StatusOK, out)
		return
	}
	runs, total := h.Runs.ListPageByTags(statuses, wf, projectID, tags, pg.Page, pg.PageSize, sort, order)
	labels := h.Runs.CurrentNodeLabels(runs)
	items := make([]gin.H, 0, len(runs))
	for _, r := range runs {
		items = append(items, runSummaryDTO(r, labels[r.ID]))
	}
	c.JSON(http.StatusOK, paginatedResponse(items, int(total), pg.Page, pg.PageSize))
}

// parseRunListSort returns whitelist sort/order for ListRuns.
// Both must be valid as a pair; otherwise empty strings signal default order.
func parseRunListSort(sort, order string) (string, string) {
	sort = strings.TrimSpace(sort)
	order = strings.ToLower(strings.TrimSpace(order))
	switch sort {
	case "started_at", "priority":
	default:
		return "", ""
	}
	switch order {
	case "asc", "desc":
		return sort, order
	default:
		return "", ""
	}
}

func (h *Handlers) GetRun(c *gin.Context) {
	run, ok := h.Runs.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, h.runDetailDTO(run))
}

func (h *Handlers) CancelRun(c *gin.Context) {
	runID := c.Param("id")
	run, ok := h.Runs.Get(runID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	actor := h.auditActorFromContext(c)
	if err := h.Eng.Cancel(runID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	projectID := ""
	if h.WF != nil {
		if wf, wOk := h.WF.Get(run.WorkflowID); wOk {
			projectID = wf.ProjectID
		}
	}
	if projectID != "" {
		h.recordAudit(services.AuditRecord{
			ProjectID:    projectID,
			Actor:        actor,
			Action:       models.AuditActionRunCancel,
			ResourceType: "run",
			ResourceID:   runID,
			RunID:        runID,
			Outcome:      models.AuditOutcomeOK,
			Summary:      "cancel run",
			Payload:      map[string]any{"workflowId": run.WorkflowID, "runId": runID},
		})
	}
	c.JSON(http.StatusOK, gin.H{"status": "cancelled"})
}

// DeleteRun hard-deletes a completed/failed/cancelled run and its associated
// data. Missing id → 404; non-deletable status → 409; success → 200
// {status:deleted} (aligned with DeleteWorkflow). Permission matches
// cancel/resume (same /api session auth).
func (h *Handlers) DeleteRun(c *gin.Context) {
	if err := h.Runs.Delete(c.Param("id")); err != nil {
		switch {
		case errors.Is(err, services.ErrRunNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		case errors.Is(err, services.ErrRunNotDeletable):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

type updateRunPriorityBody struct {
	Priority string `json:"priority"`
}

// UpdateRunPriority changes the admission priority of a non-terminal run.
// Permission matches cancel/resume (same /api session auth). Terminal runs
// (completed/failed/cancelled) are rejected with a clear error.
func (h *Handlers) UpdateRunPriority(c *gin.Context) {
	runID := c.Param("id")
	if _, ok := h.Runs.Get(runID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var b updateRunPriorityBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}
	run, err := h.Eng.UpdateRunPriority(runID, b.Priority)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":       run.ID,
		"status":   run.Status,
		"priority": models.PriorityLabel(run.Priority),
	})
}

type resumeRunBody struct {
	// NodeID chooses where to continue; empty resumes from the node that failed.
	NodeID string `json:"nodeId"`
}

// ResumeRun continues a failed/cancelled run from a node (default: the failed
// one), reusing everything the original run already produced.
func (h *Handlers) ResumeRun(c *gin.Context) {
	var b resumeRunBody
	if err := c.ShouldBindJSON(&b); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON: " + err.Error()})
		return
	}
	runID := c.Param("id")
	if _, ok := h.Runs.Get(runID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err := h.Eng.ResumeFrom(runID, b.NodeID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "resumed"})
}

func (h *Handlers) RunVariables(c *gin.Context) {
	c.JSON(http.StatusOK, h.Runs.Variables(c.Param("id")))
}

func (h *Handlers) RunArtifacts(c *gin.Context) {
	c.JSON(http.StatusOK, h.Arts.ByRun(c.Param("id")))
}

// writeNodeEventsLiveReadErr returns HTTP 200 with an explicit unavailable marker
// so browsers do not log Failed to load resource for expected transient bridge
// failures while a live sandbox is registered. Callers distinguish this from
// a genuine empty timeline via unavailable=true (never a silent empty success).
func writeNodeEventsLiveReadErr(c *gin.Context, paginated bool) {
	body := gin.H{
		"events":      []models.AcpEvent{},
		"live":        false,
		"unavailable": true,
		"error":       "live event log read failed",
	}
	if paginated {
		body["nextCursor"] = ""
		body["hasMore"] = false
	}
	c.JSON(http.StatusOK, body)
}

// NodeEvents returns a run node's agent event log. While the node is executing
// it is read live from its sandbox (so it survives a UI refresh / re-entry);
// once the sandbox is gone it falls back to the node's persisted final snapshot.
// When a live sandbox is registered but the bridge read fails, the handler
// returns HTTP 200 with unavailable=true so the UI can show a rehydrate failure
// without polluting the browser console with 502 Failed to load resource.
func (h *Handlers) NodeEvents(c *gin.Context) {
	runID := c.Param("id")
	nodeID := c.Param("nodeId")
	cp, ok := parseCursorPagination(c)
	if !ok {
		return
	}
	if !cp.Active {
		ev, live, err := h.Eng.LiveNodeEvents(c.Request.Context(), runID, nodeID)
		if err != nil {
			_ = c.Error(err)
			writeNodeEventsLiveReadErr(c, false)
			return
		}
		if live {
			c.JSON(http.StatusOK, gin.H{"events": ev, "live": true})
			return
		}
		if sr, ok := h.Runs.StateRun(runID, nodeID); ok {
			c.JSON(http.StatusOK, gin.H{"events": previewPromptEvents(sr.Events), "live": false})
			return
		}
		c.JSON(http.StatusOK, gin.H{"events": []models.AcpEvent{}, "live": false})
		return
	}

	ev, next, hasMore, live, err := h.Eng.LiveNodeEventsPage(c.Request.Context(), runID, nodeID, cp.Cursor, cp.Limit)
	if err != nil {
		_ = c.Error(err)
		writeNodeEventsLiveReadErr(c, true)
		return
	}
	if live {
		c.JSON(http.StatusOK, gin.H{"events": ev, "nextCursor": next, "hasMore": hasMore, "live": true})
		return
	}
	if sr, ok := h.Runs.StateRun(runID, nodeID); ok {
		ev, next, hasMore := pagePersistedEvents(previewPromptEvents(sr.Events), cp.Cursor, cp.Limit)
		c.JSON(http.StatusOK, gin.H{"events": ev, "nextCursor": next, "hasMore": hasMore, "live": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": []models.AcpEvent{}, "nextCursor": "", "hasMore": false, "live": false})
}
