package handlers

import (
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/engine"

	"github.com/gin-gonic/gin"
)

type runArtifactSaveBody struct {
	Content *string `json:"content"`
}

// SaveRunArtifact stores a human edit of one run artifact (waiting_human with
// an idle session, or failed). Optional If-Match enables conflict detection.
func (h *Handlers) SaveRunArtifact(c *gin.Context) {
	var b runArtifactSaveBody
	if err := c.ShouldBindJSON(&b); err != nil || b.Content == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体须为 {content}", "code": engine.ArtifactEditCodeInvalid})
		return
	}
	actor := h.auditActorFromContext(c)
	username := ""
	if !actor.Unattributable {
		username = actor.Username
	}
	ifMatch := strings.TrimSpace(c.GetHeader("If-Match"))
	res, err := h.Eng.SaveRunArtifact(c.Param("id"), c.Param("name"), *b.Content, ifMatch, username)
	if err != nil {
		_ = c.Error(err)
		ae, ok := engine.AsArtifactEditError(err)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		body := gin.H{"error": ae.Msg, "code": ae.Code}
		status := http.StatusBadRequest
		switch ae.Code {
		case engine.ArtifactEditCodeNotFound:
			status = http.StatusNotFound
		case engine.ArtifactEditCodeRunNotEditable, engine.ArtifactEditCodeSessionBusy:
			status = http.StatusConflict
		case engine.ArtifactEditCodeConflict:
			status = http.StatusConflict
			body["revision"] = ae.Revision
		}
		if ae.Field != "" {
			body["field"] = ae.Field
		}
		c.JSON(status, body)
		return
	}
	c.Header("ETag", res.ETag)
	c.JSON(http.StatusOK, gin.H{
		"id": res.ID, "name": res.Name, "kind": res.Kind, "sizeBytes": res.SizeBytes,
		"updatedAt": res.UpdatedAt, "etag": res.ETag, "nodeId": res.NodeID,
		"content": res.Content, "revision": res.Revision, "changed": res.Changed,
	})
}
