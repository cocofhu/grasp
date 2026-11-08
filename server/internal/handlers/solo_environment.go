package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h *Handlers) StopSoloEnvironment(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	view, err := h.Solo.StopEnvironment(c.Request.Context(), c.Param("id"), user)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (h *Handlers) ResumeSoloEnvironment(c *gin.Context) {
	user, ok := h.soloReady(c)
	if !ok {
		return
	}
	view, err := h.Solo.ResumeEnvironment(c.Request.Context(), c.Param("id"), user)
	if err != nil {
		writeSoloErr(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}
