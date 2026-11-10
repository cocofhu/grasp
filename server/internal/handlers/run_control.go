package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h *Handlers) PauseWorkflowRun(c *gin.Context)        { h.controlWorkflowRun(c, false) }
func (h *Handlers) ResumePausedWorkflowRun(c *gin.Context) { h.controlWorkflowRun(c, true) }
func (h *Handlers) controlWorkflowRun(c *gin.Context, resume bool) {
	var body struct {
		Version *uint64 `json:"version"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Version == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "version required"})
		return
	}
	var err error
	if resume {
		err = h.Eng.ResumePausedRun(c.Param("id"), *body.Version)
	} else {
		err = h.Eng.PauseRun(c.Param("id"), *body.Version)
	}
	if err != nil {
		writeAdmissionErr(c, err)
		return
	}
	run, ok := h.Runs.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
		return
	}
	c.JSON(http.StatusOK, runSummaryDTO(run, ""))
}
