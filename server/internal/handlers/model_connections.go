package handlers

import (
	"errors"
	"net/http"

	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

// ModelConnectionHandlers is registered inside the existing authenticated
// workspace router; it exposes metadata only and never resolves secret values.
type ModelConnectionHandlers struct {
	Service *services.ModelConnectionService
}

func (h *ModelConnectionHandlers) List(c *gin.Context) {
	items, err := h.Service.List()
	if err != nil {
		writeModelConnectionErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h *ModelConnectionHandlers) Get(c *gin.Context) {
	item, err := h.Service.Get(c.Param("connectionId"))
	if err != nil {
		writeModelConnectionErr(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *ModelConnectionHandlers) Create(c *gin.Context) {
	var input modelrouting.Connection
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model connection request"})
		return
	}
	item, err := h.Service.Create(input)
	if err != nil {
		writeModelConnectionErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *ModelConnectionHandlers) Update(c *gin.Context) {
	var input modelrouting.Connection
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model connection request"})
		return
	}
	item, err := h.Service.Update(c.Param("connectionId"), input)
	if err != nil {
		writeModelConnectionErr(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *ModelConnectionHandlers) Delete(c *gin.Context) {
	if err := h.Service.Delete(c.Param("connectionId")); err != nil {
		writeModelConnectionErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *ModelConnectionHandlers) Capabilities(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"items": modelrouting.Capabilities()})
}

func writeModelConnectionErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrModelConnectionNotFound), errors.Is(err, services.ErrCredentialNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrModelConnectionInvalid), errors.Is(err, services.ErrModelBindingInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "model connection operation failed"})
	}
}
