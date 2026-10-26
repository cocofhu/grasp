package handlers

import (
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/clicatalog"
	"github.com/gin-gonic/gin"
)

// ListCliBackendModels answers a tool's platform model choices. OpenCode Zen
// uses only its official provider from the shared catalog; other tools have a
// shipped shortlist. Unknown backends yield an empty list.
func (h *Handlers) ListCliBackendModels(c *gin.Context) {
	if strings.ToLower(strings.TrimSpace(c.Param("backend"))) == "opencode" && h.OpenCodeCatalog != nil {
		models, err := h.OpenCodeCatalog.Models(c.Request.Context(), "opencode")
		out := make([]clicatalog.Model, 0, len(models))
		for _, model := range models {
			if model.ID == "" || strings.Contains(model.ID, "/") {
				continue
			}
			out = append(out, clicatalog.Model{ID: "opencode/" + model.ID, Name: model.Name, Vendor: "opencode"})
		}
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"models": out, "error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"models": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": clicatalog.Models(c.Param("backend"))})
}
