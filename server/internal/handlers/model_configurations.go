package handlers

import (
	"errors"
	"net/http"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

// ModelConfigurationHandlers exposes saved tool/model combinations without secrets.
type ModelConfigurationHandlers struct {
	Service *services.ModelConfigurationService
}

func (h *ModelConfigurationHandlers) List(c *gin.Context) {
	items, err := h.Service.List()
	if err != nil {
		writeModelConfigurationErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *ModelConfigurationHandlers) Get(c *gin.Context) {
	item, err := h.Service.Get(c.Param("configurationId"))
	if err != nil {
		writeModelConfigurationErr(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *ModelConfigurationHandlers) Create(c *gin.Context) {
	var input modelrouting.Configuration
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model configuration request"})
		return
	}
	item, err := h.Service.Create(input)
	if err != nil {
		writeModelConfigurationErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *ModelConfigurationHandlers) Update(c *gin.Context) {
	var input modelrouting.Configuration
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model configuration request"})
		return
	}
	item, err := h.Service.Update(c.Param("configurationId"), input)
	if err != nil {
		writeModelConfigurationErr(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *ModelConfigurationHandlers) Delete(c *gin.Context) {
	if err := h.Service.Delete(c.Param("configurationId")); err != nil {
		writeModelConfigurationErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func writeModelConfigurationErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrModelConfigurationNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrModelConfigurationInvalid), errors.Is(err, services.ErrModelBindingInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "model configuration operation failed"})
	}
}
