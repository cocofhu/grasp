package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"sandbox-gateway/internal/driver"
	"sandbox-gateway/internal/service"
	"sandbox-gateway/internal/store"
)

func (h *Handler) environmentReleases(c *gin.Context) *service.EnvironmentReleases {
	r := h.svc.EnvironmentReleases()
	if r == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"code": "environment_release_unsupported", "reason": "controller_unavailable"})
		return nil
	}
	return r
}
func environmentReleaseError(c *gin.Context, err error) {
	status, code := http.StatusServiceUnavailable, "environment_release_unavailable"
	switch {
	case errors.Is(err, driver.ErrEnvironmentReleaseUnsupported):
		status, code = http.StatusNotImplemented, "environment_release_unsupported"
	case errors.Is(err, service.ErrReleaseInvalid):
		status, code = http.StatusBadRequest, "invalid_environment_release"
	case errors.Is(err, service.ErrReleaseConflict), errors.Is(err, driver.ErrEnvironmentReleaseConflict):
		status, code = http.StatusConflict, "environment_release_scope_changed"
	case errors.Is(err, service.ErrReleaseOwner):
		status, code = http.StatusConflict, "environment_release_owner_lost"
	case errors.Is(err, driver.ErrEnvironmentReleasePending):
		status, code = http.StatusConflict, "environment_release_pending"
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, store.ErrNotFound):
		status, code = http.StatusNotFound, "environment_release_not_found"
	}
	c.JSON(status, gin.H{"code": code})
}
func releaseDecode(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(v)
	if err == nil {
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			err = service.ErrReleaseInvalid
		}
	}
	if err != nil {
		environmentReleaseError(c, service.ErrReleaseInvalid)
		return false
	}
	return true
}
func (h *Handler) ReleaseCapabilities(c *gin.Context) {
	r := h.environmentReleases(c)
	if r == nil {
		return
	}
	c.JSON(http.StatusOK, r.Capabilities(c.Request.Context(), c.Param("id")))
}
func (h *Handler) PreviewEnvironmentRelease(c *gin.Context) {
	r := h.environmentReleases(c)
	if r == nil {
		return
	}
	var input service.EnvironmentReleasePrepare
	if !releaseDecode(c, &input) {
		return
	}
	view, err := r.Preview(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		environmentReleaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}
func (h *Handler) ConfirmEnvironmentRelease(c *gin.Context) {
	r := h.environmentReleases(c)
	if r == nil {
		return
	}
	var input service.EnvironmentReleaseConfirm
	if !releaseDecode(c, &input) {
		return
	}
	view, err := r.Confirm(c.Request.Context(), c.Param("id"), c.Param("releaseId"), input)
	if err != nil {
		environmentReleaseError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, view)
}
func (h *Handler) RenewEnvironmentRelease(c *gin.Context) {
	r := h.environmentReleases(c)
	if r == nil {
		return
	}
	var input service.EnvironmentReleaseRenew
	if !releaseDecode(c, &input) {
		return
	}
	view, err := r.Renew(c.Request.Context(), c.Param("id"), c.Param("releaseId"), input)
	if err != nil {
		environmentReleaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}
func (h *Handler) GetEnvironmentRelease(c *gin.Context) {
	r := h.environmentReleases(c)
	if r == nil {
		return
	}
	view, err := r.Get(c.Request.Context(), c.Param("id"), c.Param("releaseId"))
	if err != nil {
		environmentReleaseError(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}
