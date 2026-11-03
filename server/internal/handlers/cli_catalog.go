package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/clicatalog"
	"github.com/gin-gonic/gin"
)

// ListCliBackendModels answers a tool's platform model choices. OpenCode official
// mode merges provider opencode and opencode-go from the shared catalog; other
// tools have a shipped shortlist. Unknown backends yield an empty list.
func (h *Handlers) ListCliBackendModels(c *gin.Context) {
	models, err := h.cliBackendModels(c.Request.Context(), c.Param("backend"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"models": models, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": models})
}

func (h *Handlers) cliBackendModels(ctx context.Context, backend string) ([]clicatalog.Model, error) {
	if strings.ToLower(strings.TrimSpace(backend)) == "opencode" && h.OpenCodeCatalog != nil {
		out := make([]clicatalog.Model, 0)
		var failure error
		for _, providerID := range []string{"opencode", "opencode-go"} {
			models, err := h.OpenCodeCatalog.Models(ctx, providerID)
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
		return out, failure
	}
	return clicatalog.Models(backend), nil
}
