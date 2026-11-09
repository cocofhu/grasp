package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// WorkspaceBindingHandlers are composed separately so receipt/history reads
// retain the application's ordinary principal policy and never wake a guest.
type WorkspaceBindingHandlers struct {
	Service *services.WorkspaceBindingService
	Auth    *auth.Service
}

func (h *WorkspaceBindingHandlers) actor(c *gin.Context) (string, bool) {
	if c.GetString(soloEmbedUserKey) != "" {
		c.JSON(http.StatusForbidden, gin.H{"code": "workspace_owner_session_required"})
		return "", false
	}
	if h == nil || h.Service == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "workspace_activation_unavailable"})
		return "", false
	}
	if h.Auth == nil {
		return "anonymous", true
	}
	session, ok := h.Auth.RequireSession(c)
	if !ok {
		return "", false
	}
	return session.Username, true
}
func workspaceBindingError(c *gin.Context, err error) {
	status, code := http.StatusInternalServerError, "workspace_operation_failed"
	var solo *services.SoloError
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, code = http.StatusNotFound, "workspace_operation_not_found"
	case errors.Is(err, services.ErrWorkspaceJobInvalid):
		status, code = http.StatusBadRequest, "workspace_operation_invalid"
	case errors.Is(err, services.ErrWorkspaceJobConflict):
		status, code = http.StatusConflict, "workspace_operation_conflict"
	case errors.Is(err, services.ErrWorkspaceJobUnknown):
		status, code = http.StatusConflict, "workspace_switch_pending"
	case errors.Is(err, services.ErrWorkspaceJobUnsupported):
		status, code = http.StatusUnprocessableEntity, "workspace_activation_unavailable"
	case errors.Is(err, services.ErrWorkspaceJobAuthority):
		status, code = http.StatusServiceUnavailable, "workspace_execution_unavailable"
	case errors.As(err, &solo):
		status, code = solo.Status, solo.Code
	}
	c.JSON(status, gin.H{"code": code})
}
func (h *WorkspaceBindingHandlers) Current(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	value, err := h.Service.CurrentView(c.Request.Context(), actor, c.Param("id"))
	if err != nil {
		workspaceBindingError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}
func (h *WorkspaceBindingHandlers) List(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	value, err := h.Service.ListOperations(c.Request.Context(), actor, c.Param("id"))
	if err != nil {
		workspaceBindingError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}
func (h *WorkspaceBindingHandlers) Get(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	value, err := h.Service.OperationView(c.Request.Context(), actor, c.Param("id"), c.Param("operationId"))
	if err != nil {
		workspaceBindingError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}
func (h *WorkspaceBindingHandlers) ByRequest(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	value, err := h.Service.OperationByRequest(c.Request.Context(), actor, c.Param("id"), c.Param("requestId"))
	if err != nil {
		workspaceBindingError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}

type workspaceBindingActions interface {
	Admit(context.Context, string, services.WorkspaceBindingInput) (models.WorkspaceBindingOperation, error)
	Reconcile(context.Context, string, string, string) (models.WorkspaceBindingOperation, error)
}

var _ workspaceBindingActions = (*services.WorkspaceBindingService)(nil)

func (h *WorkspaceBindingHandlers) Admit(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	var body struct {
		ClientRequestID  string `json:"clientRequestId"`
		Action           string `json:"action"`
		ExpectedBinding  string `json:"expectedBinding"`
		RestoreID        string `json:"restoreId,omitempty"`
		TargetBindingID  string `json:"targetBindingId,omitempty"`
		ReturnSnapshotID string `json:"returnSnapshotId"`
		DurationMillis   int64  `json:"durationMillis,omitempty"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&body) != nil || !errors.Is(d.Decode(&extra), io.EOF) || body.DurationMillis < 0 || body.DurationMillis > 30*60*1000 {
		workspaceBindingError(c, services.ErrWorkspaceJobInvalid)
		return
	}
	if body.DurationMillis == 0 {
		body.DurationMillis = 60000
	}
	input := services.WorkspaceBindingInput{ClientRequestID: body.ClientRequestID, SoloID: c.Param("id"), Action: body.Action, ExpectedBinding: body.ExpectedBinding, RestoreID: body.RestoreID, TargetBindingID: body.TargetBindingID, ReturnSnapshotID: body.ReturnSnapshotID, Duration: time.Duration(body.DurationMillis) * time.Millisecond}
	row, err := h.Service.Admit(c.Request.Context(), actor, input)
	if err != nil {
		workspaceBindingError(c, err)
		return
	}
	value, err := h.Service.OperationView(c.Request.Context(), actor, row.SoloID, row.ID)
	if err != nil {
		workspaceBindingError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, value)
}
func (h *WorkspaceBindingHandlers) Reconcile(c *gin.Context) {
	actor, ok := h.actor(c)
	if !ok {
		return
	}
	row, err := h.Service.Reconcile(c.Request.Context(), actor, c.Param("id"), c.Param("operationId"))
	if err != nil {
		workspaceBindingError(c, err)
		return
	}
	value, err := h.Service.OperationView(c.Request.Context(), actor, row.SoloID, row.ID)
	if err != nil {
		workspaceBindingError(c, err)
		return
	}
	c.JSON(http.StatusOK, value)
}
