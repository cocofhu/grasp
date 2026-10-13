package handlers

import (
	"net/http"

	"github.com/cocofhu/grasp/internal/clicatalog"
	"github.com/gin-gonic/gin"
)

// ListCliBackendModels answers the shipped model shortlist for a coding CLI
// backend. An unknown backend yields an empty list: the picker then offers
// only "auto" and a hand-typed id.
func (h *Handlers) ListCliBackendModels(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"models": clicatalog.Models(c.Param("backend"))})
}
