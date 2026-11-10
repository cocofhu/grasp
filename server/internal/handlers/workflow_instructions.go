package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func writeInstructionError(c *gin.Context, err error) {
	var typed *engine.InstructionError
	if errors.As(err, &typed) {
		status := http.StatusConflict
		if typed.Code == "invalid_instruction" {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"code": typed.Code, "error": typed.Message})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "run_not_found", "error": "工作流执行不存在"})
		return
	}
	_ = c.Error(err)
	c.JSON(http.StatusInternalServerError, gin.H{"code": "instruction_not_persisted", "error": "补充要求未能持久化，请保留草稿后重试"})
}

// This cookie/session-authenticated route is deliberately absent from Solo's
// MCP tool set and from bearer/embed shares. Instructions belong to the shared
// Run; no private origin Solo transcript is read or copied.
func (h *Handlers) SubmitWorkflowInstruction(c *gin.Context) {
	user, ok := h.sessionUser(c)
	if !ok {
		return
	}
	if h.Eng == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "workflow engine unavailable"})
		return
	}
	var input engine.InstructionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid instruction"})
		return
	}
	item, err := h.Eng.EnqueueInstruction(c.Request.Context(), c.Param("id"), c.Param("nodeId"), user, input)
	if err != nil {
		writeInstructionError(c, err)
		return
	}
	h.recordAudit(services.AuditRecord{Actor: services.ActorFromUsername(user), Action: "node.instruction", ResourceType: "run", ResourceID: item.RunID, RunID: item.RunID, Outcome: "ok", Summary: "supplement workflow node " + item.NodeID, Payload: map[string]any{"nodeId": item.NodeID, "iteration": item.Iteration, "generation": item.Generation, "instructionId": item.ID, "state": item.State}})
	c.JSON(http.StatusOK, item)
}

func (h *Handlers) WorkflowInstructionHistory(c *gin.Context) {
	if _, ok := h.sessionUser(c); !ok {
		return
	}
	if h.Eng == nil || h.Runs == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "workflow engine unavailable"})
		return
	}
	if _, ok := h.Runs.Get(c.Param("id")); !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": "run_not_found", "error": "工作流执行不存在"})
		return
	}
	before, err := strconv.ParseUint(c.DefaultQuery("before", "0"), 10, 64)
	generation := c.Query("generation")
	if err != nil || len(generation) > 36 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid instruction cursor"})
		return
	}
	box, items, more, err := h.Eng.InstructionHistory(c.Request.Context(), c.Param("id"), c.Param("nodeId"), generation, before)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusOK, gin.H{"target": nil, "items": []any{}, "hasMore": false})
		return
	}
	if err != nil {
		writeInstructionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"target": box, "items": items, "hasMore": more})
}
