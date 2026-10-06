package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

// ListArtifacts pages one project's artifacts. projectId is required;
// workflowId narrows to one workflow, session=1 to workflow-less artifacts.
func (h *Handlers) ListArtifacts(c *gin.Context) {
	f := services.ArtifactFilter{
		ProjectID:  c.Query("projectId"),
		WorkflowID: c.Query("workflowId"),
		Session:    c.Query("session") == "1",
		Q:          c.Query("q"),
	}
	if f.ProjectID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "projectId is required"})
		return
	}
	if f.Session && f.WorkflowID != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workflowId and session are mutually exclusive"})
		return
	}
	pg, ok := parsePagination(c)
	if !ok {
		return
	}
	if c.Query("groupBy") == "run" {
		arts, total := h.Arts.AllPageByRun(f, pg.Page, pg.PageSize)
		c.JSON(http.StatusOK, paginatedResponse(arts, int(total), pg.Page, pg.PageSize))
		return
	}
	arts, total := h.Arts.AllPage(f, pg.Page, pg.PageSize)
	c.JSON(http.StatusOK, paginatedResponse(arts, int(total), pg.Page, pg.PageSize))
}

// ArtifactTree returns per-project / per-workflow artifact counts for the
// Artifacts page sidebar.
func (h *Handlers) ArtifactTree(c *gin.Context) {
	tree, err := h.Arts.Tree()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, tree)
}

// ArtifactContent returns a single artifact's full record including its
// content (the list/run DTOs omit content to stay lightweight).
func (h *Handlers) ArtifactContent(c *gin.Context) {
	a, ok := h.Arts.GetByID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	content := a.Content
	etag := engine.ArtifactETag(content, a.SizeBytes, a.UpdatedAt)
	out := gin.H{
		"id": a.ID, "runId": a.RunID, "nodeId": a.NodeID, "projectId": a.ProjectID, "workflowId": a.WorkflowID, "workflowName": a.WorkflowName,
		"name": a.Name, "kind": a.Kind, "sizeBytes": a.SizeBytes,
		"createdAt": a.CreatedAt, "content": content, "etag": etag,
	}
	rev := a.Revision
	if rev < 1 {
		rev = 1
	}
	out["revision"] = rev
	if !a.UpdatedAt.IsZero() {
		out["updatedAt"] = a.UpdatedAt
	}
	c.Header("ETag", etag)
	c.JSON(http.StatusOK, out)
}

// ArtifactVersions lists archived snapshots for one artifact (metadata only).
func (h *Handlers) ArtifactVersions(c *gin.Context) {
	a, ok := h.Arts.GetByID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, h.Arts.ListVersions(a.ID))
}

// ArtifactVersionContent returns one archived snapshot including its content.
func (h *Handlers) ArtifactVersionContent(c *gin.Context) {
	a, ok := h.Arts.GetByID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	rev, err := strconv.Atoi(c.Param("rev"))
	if err != nil || rev < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid revision"})
		return
	}
	ver, ok := h.Arts.GetVersion(a.ID, rev)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"artifactId": ver.ArtifactID,
		"revision":   ver.Revision,
		"nodeId":     ver.NodeID,
		"kind":       ver.Kind,
		"sizeBytes":  ver.SizeBytes,
		"createdAt":  ver.CreatedAt,
		"content":    ver.Content,
	})
}

func (h *Handlers) DownloadArtifact(c *gin.Context) {
	a, ok := h.Arts.GetByID(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	body, mime := decodeArtifactDownloadBody(a)
	c.Header("Content-Disposition", "attachment; filename="+a.Name)
	c.Data(http.StatusOK, mime, body)
}

// DeleteArtifact hard-deletes one artifact by id. Success is 204 No Content
// (no body). Missing id → 404; owning run not terminal → 409.
func (h *Handlers) DeleteArtifact(c *gin.Context) {
	if err := h.Arts.DeleteByID(c.Param("id")); err != nil {
		switch {
		case errors.Is(err, services.ErrArtifactNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		case errors.Is(err, services.ErrArtifactRunNotTerminal):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.Status(http.StatusNoContent)
}
