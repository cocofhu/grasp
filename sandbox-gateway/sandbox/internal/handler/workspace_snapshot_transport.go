package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"backend/internal/workspacejob"
	"backend/internal/workspacesnapshot"
	"github.com/gin-gonic/gin"
)

const snapshotFenceHeader = "X-Grasp-Workspace-Fence"

// WorkspaceSnapshotTransport must share bridge authentication with WorkspaceJobs.
// It exposes two fixed artifacts and bounded ranges, never filesystem paths.
type WorkspaceSnapshotTransport struct{ Controller *workspacesnapshot.Controller }

func (h *WorkspaceSnapshotTransport) ready(c *gin.Context) bool {
	if h.Controller != nil {
		return true
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"code": "workspace_files_unavailable"})
	return false
}

func snapshotTransportError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, workspacesnapshot.ErrPending):
		c.JSON(http.StatusTooEarly, gin.H{"code": "workspace_files_pending"})
	case errors.Is(err, workspacesnapshot.ErrLimit):
		jobError(c, workspacejob.ErrLimit)
	case errors.Is(err, workspacesnapshot.ErrUnsafe), errors.Is(err, workspacesnapshot.ErrIntegrity):
		jobError(c, workspacejob.ErrInvalid)
	default:
		jobError(c, err)
	}
}

func snapshotFence(c *gin.Context) (workspacejob.RenewRequest, error) {
	var request workspacejob.RenewRequest
	value := c.GetHeader(snapshotFenceHeader)
	if len(value) == 0 || len(value) > 4096 {
		return request, workspacejob.ErrInvalid
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || json.Unmarshal(data, &request) != nil {
		return request, workspacejob.ErrInvalid
	}
	return request, nil
}

func (h *WorkspaceSnapshotTransport) Capabilities(c *gin.Context) {
	if h.ready(c) {
		c.JSON(http.StatusOK, h.Controller.Capabilities())
	}
}

func (h *WorkspaceSnapshotTransport) Read(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	fence, err := snapshotFence(c)
	offset, offsetErr := strconv.ParseInt(c.Query("offset"), 10, 64)
	length, lengthErr := strconv.Atoi(c.Query("length"))
	if err != nil || offsetErr != nil || lengthErr != nil {
		snapshotTransportError(c, workspacejob.ErrInvalid)
		return
	}
	chunk, err := h.Controller.ReadChunk(c.Param("jobId"), fence, c.Param("part"), offset, length)
	if err != nil {
		snapshotTransportError(c, err)
		return
	}
	c.Header("X-Grasp-Snapshot-Offset", strconv.FormatInt(chunk.Offset, 10))
	c.Header("X-Grasp-Snapshot-SHA256", chunk.SHA256)
	c.Header("X-Grasp-Snapshot-EOF", strconv.FormatBool(chunk.EOF))
	c.Data(http.StatusOK, "application/octet-stream", chunk.Data)
}

func (h *WorkspaceSnapshotTransport) Write(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	fence, err := snapshotFence(c)
	offset, offsetErr := strconv.ParseInt(c.Query("offset"), 10, 64)
	if err != nil || offsetErr != nil {
		snapshotTransportError(c, workspacejob.ErrInvalid)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, workspacesnapshot.MaxChunkBytes)
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		snapshotTransportError(c, workspacejob.ErrLimit)
		return
	}
	ack, err := h.Controller.WriteChunk(c.Param("jobId"), fence, c.Param("part"), offset, data, c.GetHeader("X-Grasp-Snapshot-SHA256"))
	if err != nil {
		snapshotTransportError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"offset": ack})
}

func (h *WorkspaceSnapshotTransport) Finish(c *gin.Context) {
	if !h.ready(c) {
		return
	}
	fence, err := snapshotFence(c)
	if err == nil {
		err = h.Controller.Finish(c.Param("jobId"), fence)
	}
	if err != nil {
		snapshotTransportError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"accepted": true})
}
