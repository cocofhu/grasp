package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/nodereg"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

type gateResumeBody struct {
	Action string         `json:"action"`
	Form   map[string]any `json:"form"`
}

func (h *Handlers) ResumeGate(c *gin.Context) {
	var b gateResumeBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	actor := h.auditActorFromContext(c)
	reviewer := ""
	if !actor.Unattributable {
		reviewer = actor.Username
	}
	if err := h.Eng.ResumeGateAs(c.Param("id"), c.Param("nodeId"), b.Action, b.Form, reviewer); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "resumed"})
}

type gateArtifactSaveBody struct {
	Content string `json:"content"`
}

// ListGatePrimaryArtifacts returns the editable primary products for a pending gate.
func (h *Handlers) ListGatePrimaryArtifacts(c *gin.Context) {
	items, err := h.Eng.ListGatePrimaryProducts(c.Param("id"), c.Param("nodeId"))
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if items == nil {
		c.JSON(http.StatusOK, gin.H{"items": []any{}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// SaveGateArtifact updates a gate-scoped primary artifact (waiting_human only).
// Optional If-Match header enables external-change detection (409 on mismatch).
func (h *Handlers) SaveGateArtifact(c *gin.Context) {
	var b gateArtifactSaveBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name := c.Param("name")
	ifMatch := strings.TrimSpace(c.GetHeader("If-Match"))
	res, err := h.Eng.SaveGateArtifact(c.Param("id"), c.Param("nodeId"), name, b.Content, ifMatch)
	if err != nil {
		_ = c.Error(err)
		if engine.IsArtifactConflict(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Header("ETag", res.ETag)
	c.JSON(http.StatusOK, gin.H{
		"id": res.ID, "name": res.Name, "kind": res.Kind, "sizeBytes": res.SizeBytes,
		"updatedAt": res.UpdatedAt, "etag": res.ETag, "nodeId": res.NodeID,
		"content": res.Content,
	})
}

// SaveAnnotationArtifact upserts the CommentPin package (preview_annotations.json).
// Independent of SaveGateArtifact whitelist and PreviewIssue lifecycle.
func (h *Handlers) SaveAnnotationArtifact(c *gin.Context) {
	var doc engine.AnnotationArtifactDoc
	if err := c.ShouldBindJSON(&doc); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	res, err := h.Eng.SaveAnnotationArtifact(c.Param("id"), c.Param("nodeId"), doc)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Header("ETag", res.ETag)
	c.JSON(http.StatusOK, gin.H{
		"id": res.ID, "name": res.Name, "kind": res.Kind, "sizeBytes": res.SizeBytes,
		"updatedAt": res.UpdatedAt, "etag": res.ETag, "nodeId": res.NodeID,
		"content": res.Content, "cleared": res.Cleared,
	})
}

type reactReplyBody struct {
	Text   string               `json:"text"`
	Images []models.PromptImage `json:"images"`
	// Annotations are precise field/element references (JSON path or DOM
	// selector + note) the human attached to this review turn.
	Annotations []models.ReactAnnotation `json:"annotations"`
	// Force finishes the clarification/review early: the agent is asked to wrap
	// up and the node completes regardless of any further questions. For a
	// review node force=true is "确认并流转"; force=false is one in-place edit.
	Force bool `json:"force"`
	// RetryLast re-runs the latest human turn without inserting another human
	// row (cover-this-turn retry after an empty/failed agent reply).
	RetryLast bool `json:"retryLast"`
	// AbortRunning, with Force, cancels a sandbox turn the platform no longer
	// tracks (orphan CLI) before confirming. Without it a busy sandbox is 409.
	AbortRunning bool `json:"abortRunning"`
}

func (h *Handlers) ReactReply(c *gin.Context) {
	var b reactReplyBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	runID, nodeID := c.Param("id"), c.Param("nodeId")
	var err error
	if b.RetryLast {
		if b.Force {
			c.JSON(http.StatusBadRequest, gin.H{"error": "retryLast cannot be combined with force"})
			return
		}
		err = h.Eng.ReactReplyRetryLastAs(sessionTurnOwner(c), runID, nodeID)
	} else if b.Force {
		err = h.Eng.ReactConfirmAs(sessionTurnOwner(c), runID, nodeID, b.Text, b.Images, b.Annotations, b.AbortRunning)
	} else {
		err = h.Eng.ReactReplyAs(sessionTurnOwner(c), runID, nodeID, b.Text, b.Images, b.Annotations, false)
	}
	if err != nil {
		writeReactReplyError(c, err)
		return
	}

	if !b.Force {
		if w, thinking := h.Eng.ReviewSessionState(runID, nodeID); thinking || w > 0 {
			c.JSON(http.StatusOK, gin.H{"status": "accepted", "waiting": w})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ReactCancel aborts the current turn. Review clears the pending FIFO (#77);
// classic clarify keeps the queue and lets the pump start the next item (Demo).
func (h *Handlers) ReactCancel(c *gin.Context) {
	runID, nodeID := c.Param("id"), c.Param("nodeId")
	clearQueue := true
	if run, ok := h.Runs.Get(runID); ok {
		if n := run.Graph.FindNode(nodeID); n != nil && nodereg.ClarifyInteractive(n.Type) {
			clearQueue = false
		}
	}
	var err error
	if clearQueue {
		err = h.Eng.CancelReviewSession(runID, nodeID)
	} else {
		err = h.Eng.CancelClarifyTurn(runID, nodeID)
	}
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type gateReactReviseBody struct {
	Text        string                   `json:"text"`
	Images      []models.PromptImage     `json:"images"`
	Annotations []models.ReactAnnotation `json:"annotations"`
}

// GateReactRevise issues a ReAct reject-and-annotate against a pending approval
// gate: the annotation/text/images are sent to the upstream producer's still-
// alive sandbox session, which edits the product in place; the gate body is
// refreshed and stays pending for further rounds.
func (h *Handlers) GateReactRevise(c *gin.Context) {
	var b gateReactReviseBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(b.Text) == "" && len(b.Images) == 0 && len(b.Annotations) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text, images, or annotations required"})
		return
	}
	runID, gateNodeID := c.Param("id"), c.Param("nodeId")
	if err := h.Eng.GateReactReviseAs(sessionTurnOwner(c), runID, gateNodeID, b.Text, b.Images, b.Annotations); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	producerID, _ := h.Eng.GateReactInfo(runID, gateNodeID)
	waiting, _ := h.Eng.ReviewSessionState(runID, producerID)
	c.JSON(http.StatusOK, gin.H{"status": "accepted", "waiting": waiting, "producerNodeId": producerID})
}

type reactQueueItemBody struct {
	ItemID string `json:"itemId"`
}

type reactQueueReorderBody struct {
	ItemIDs []string `json:"itemIds"`
}

// ReactQueueRemove drops one waiting item from the node-inline review/clarify FIFO.
func (h *Handlers) ReactQueueRemove(c *gin.Context) {
	var b reactQueueItemBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	runID, nodeID := c.Param("id"), c.Param("nodeId")
	if err := h.Eng.RemoveQueuedItem(runID, nodeID, b.ItemID); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ReactQueueReorder reorders waiting items for a node-inline review/clarify session.
func (h *Handlers) ReactQueueReorder(c *gin.Context) {
	var b reactQueueReorderBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	runID, nodeID := c.Param("id"), c.Param("nodeId")
	if err := h.Eng.ReorderQueuedItems(runID, nodeID, b.ItemIDs); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// GateReactQueueRemove drops one waiting item from the upstream producer FIFO.
func (h *Handlers) GateReactQueueRemove(c *gin.Context) {
	var b reactQueueItemBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	runID, gateNodeID := c.Param("id"), c.Param("nodeId")
	producerID, alive := h.Eng.GateReactInfo(runID, gateNodeID)
	if producerID == "" || !alive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "上游复审会话不可用"})
		return
	}
	if err := h.Eng.RemoveQueuedItem(runID, producerID, b.ItemID); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "producerNodeId": producerID})
}

// GateReactQueueReorder reorders waiting items on the upstream producer FIFO.
func (h *Handlers) GateReactQueueReorder(c *gin.Context) {
	var b reactQueueReorderBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	runID, gateNodeID := c.Param("id"), c.Param("nodeId")
	producerID, alive := h.Eng.GateReactInfo(runID, gateNodeID)
	if producerID == "" || !alive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "上游复审会话不可用"})
		return
	}
	if err := h.Eng.ReorderQueuedItems(runID, producerID, b.ItemIDs); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "producerNodeId": producerID})
}

// GateReactCancel cancels the upstream producer's review turn/queue from a gate.
func (h *Handlers) GateReactCancel(c *gin.Context) {
	runID, gateNodeID := c.Param("id"), c.Param("nodeId")
	producerID, alive := h.Eng.GateReactInfo(runID, gateNodeID)
	if producerID == "" || !alive {
		c.JSON(http.StatusBadRequest, gin.H{"error": "上游复审会话不可用"})
		return
	}
	if err := h.Eng.CancelReviewSession(runID, producerID); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "producerNodeId": producerID})
}

func (h *Handlers) ListGates(c *gin.Context) {
	wf := c.Query("wf")
	projectID := c.Query("projectId")
	tags := parseRunTags(append(c.QueryArray("tag"), c.Query("tag"))...)
	pg, ok := parsePagination(c)
	if !ok {
		return
	}
	if !pg.Active {
		items, _ := h.Runs.PendingInboxItems(wf, projectID, tags, 0, 0)
		h.attachInboxReplying(items)
		if h.GateShare != nil {
			h.GateShare.AttachInboxStatus(items)
		}
		c.JSON(http.StatusOK, items)
		return
	}
	offset := (pg.Page - 1) * pg.PageSize
	items, total := h.Runs.PendingInboxItems(wf, projectID, tags, offset, pg.PageSize)
	h.attachInboxReplying(items)
	if h.GateShare != nil {
		h.GateShare.AttachInboxStatus(items)
	}
	c.JSON(http.StatusOK, paginatedResponse(items, total, pg.Page, pg.PageSize))
}

// attachInboxReplying derives ClarifyInboxItem.state=replying from the live
// review session (busy or waiting>0), matching sessionBusy. starting wins.
func (h *Handlers) attachInboxReplying(items []any) {
	if h.Eng == nil {
		return
	}
	services.AttachInboxReplyingState(items, func(runID, nodeID string) bool {
		waiting, thinking := h.Eng.ReviewSessionState(runID, nodeID)
		return thinking || waiting > 0
	})
}

// writeReactReplyError maps confirm-path errors: a sandbox still running an
// orphan turn is 409 sandbox_busy so the UI can offer abort-and-confirm.
func writeReactReplyError(c *gin.Context, err error) {
	_ = c.Error(err)
	var busy *engine.SandboxBusyError
	if errors.As(err, &busy) {
		c.JSON(http.StatusConflict, gin.H{
			"error":       busy.Error(),
			"code":        "sandbox_busy",
			"runningOpId": busy.RunningOpID,
			"desynced":    busy.Desynced,
		})
		return
	}
	if errors.Is(err, engine.ErrSandboxBusy) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "sandbox_busy"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
}
