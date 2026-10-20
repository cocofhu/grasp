package handlers

import (
	"github.com/cocofhu/grasp/internal/auth"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h *Handlers) WorkspaceMaintenance(c *gin.Context) {
	if h.Auth != nil {
		s, ok := auth.GetSession(c)
		if !ok || !h.Auth.IsAdmin(s.Username) {
			c.JSON(http.StatusForbidden, gin.H{"error": "administrator required"})
			return
		}
	}
	if h.Maintenance == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "maintenance unavailable"})
		return
	}
	status, err := h.Maintenance(c.Request.Context(), c.Request.Method == http.MethodPost)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "status": status})
		return
	}
	c.JSON(http.StatusOK, status)
}
