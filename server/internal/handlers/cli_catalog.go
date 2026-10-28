package handlers

import (
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/clicatalog"
	"github.com/gin-gonic/gin"
)

// ListCliBackendModels answers a tool's platform model choices. OpenCode official
// mode merges provider opencode and opencode-go from the shared catalog; other
// tools have a shipped shortlist. Unknown backends yield an empty list.
func (h *Handlers) ListCliBackendModels(c *gin.Context) {
	if strings.ToLower(strings.TrimSpace(c.Param("backend"))) == "opencode" && h.OpenCodeCatalog != nil {
		out := make([]clicatalog.Model, 0)
		var failure error
		for _, providerID := range []string{"opencode", "opencode-go"} {
			models, err := h.OpenCodeCatalog.Models(c.Request.Context(), providerID)
			if err != nil && failure == nil {
				failure = err
			}
			for _, model := range models {
				id := strings.TrimSpace(model.ID)
				if id == "" || strings.Contains(id, "/") {
					continue
				}
				out = append(out, clicatalog.Model{ID: providerID + "/" + id, Name: model.Name, Vendor: providerID})
			}
		}
		if failure != nil {
			c.JSON(http.StatusOK, gin.H{"models": out, "error": failure.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"models": out})
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": clicatalog.Models(c.Param("backend"))})
}
