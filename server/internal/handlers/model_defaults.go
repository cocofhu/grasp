package handlers

import (
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h *Handlers) GetModelDefaults(c *gin.Context) {
	if h.SharedAgent == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "workspace model defaults unavailable"})
		return
	}
	cfg, err := h.SharedAgent.GetModelDefaults()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cfg)
}

func (h *Handlers) PutModelDefaults(c *gin.Context) {
	if h.SharedAgent == nil || h.ModelConnections == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "workspace model defaults unavailable"})
		return
	}
	var cfg services.ModelDefaults
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	validate := func(backend string, binding *modelrouting.Binding) error {
		_, err := h.ModelConnections.Resolve(backend, *binding)
		return err
	}
	if err := h.SharedAgent.SaveModelDefaults(cfg, validate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cfg)
}
