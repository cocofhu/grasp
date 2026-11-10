package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/store"
)

func (h *Handler) StartConfigured(c *gin.Context) {
	var opts driver.StartOptions
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&opts)
	if err == nil {
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			err = driver.ErrColdStartConflict
		}
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cold configuration"})
		return
	}
	receipt, err := h.svc.StartConfigured(c.Request.Context(), c.Param("id"), opts)
	if err != nil {
		h.coldStartError(c, err)
		return
	}
	c.JSON(http.StatusOK, receipt)
}

func (h *Handler) PreviewColdStart(c *gin.Context) {
	target, err := h.svc.PreviewColdStart(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.coldStartError(c, err)
		return
	}
	c.JSON(http.StatusOK, target)
}

func (h *Handler) GetColdStart(c *gin.Context) {
	receipt, err := h.svc.GetColdStart(c.Request.Context(), c.Param("id"), c.Param("requestId"))
	if err != nil {
		h.coldStartError(c, err)
		return
	}
	c.JSON(http.StatusOK, receipt)
}

func (h *Handler) coldStartError(c *gin.Context, err error) {
	code, message := http.StatusInternalServerError, "cold configuration unavailable"
	switch {
	case errors.Is(err, store.ErrNotFound):
		code, message = http.StatusNotFound, "cold configuration receipt not found"
	case errors.Is(err, driver.ErrColdStartUnsupported):
		code, message = http.StatusNotImplemented, "cold configuration unsupported"
	case errors.Is(err, driver.ErrColdStartUnknown):
		code, message = http.StatusConflict, "cold configuration requires read-only confirmation"
	case errors.Is(err, driver.ErrColdStartConflict):
		code, message = http.StatusConflict, "cold configuration identity changed"
	}
	c.JSON(code, gin.H{"error": message})
}
