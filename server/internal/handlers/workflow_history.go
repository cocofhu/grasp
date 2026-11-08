package handlers

import (
	"net/http"

	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

func (h *Handlers) WorkflowChat(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	run, found := h.Runs.Get(c.Param("id"))
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	id, err := h.Solo.WorkflowChat(c.Param("id"), user)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"soloId": id, "title": run.Title, "status": run.Status})
}

func (h *Handlers) ContinueWorkflow(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	if _, ok := h.Runs.Get(c.Param("id")); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var input services.SoloCreateInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid continuation configuration"})
		return
	}
	session, err := h.Solo.ContinueWorkflow(c.Request.Context(), c.Param("id"), user, input)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, session)
}
