package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func (h *Handlers) ResolveWorkspaceID(requested string) (string, error) {
	return h.Projects.ResolveWorkspaceID(requested)
}
func (h *Handlers) resolveWorkspaceID(c *gin.Context, id *string) bool {
	resolved, err := h.ResolveWorkspaceID(*id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "workspace_scope"})
		return false
	}
	*id = resolved
	return true
}
func (h *Handlers) ListWorkflowCatalog(c *gin.Context) {
	out := make([]gin.H, 0)
	for _, wf := range h.WF.Catalog() {
		out = append(out, gin.H{"id": wf.ID, "projectId": wf.ProjectID, "name": wf.Name, "description": wf.Description, "status": wf.Status(), "version": wf.Version, "publishedVersion": wf.PublishedVersion, "needsRepo": wf.NeedsRepo, "showOnHome": wf.ShowOnHome, "updatedAt": wf.UpdatedAt, "lastRunAt": wf.LastRunAt})
	}
	c.JSON(http.StatusOK, out)
}
