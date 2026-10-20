package router

import (
	"github.com/cocofhu/grasp/internal/handlers"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

func bindWorkspace(h *handlers.Handlers) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := h.ResolveWorkspaceID("")
		if err != nil {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "workspace_migration_required"})
			return
		}
		c.Params = append(c.Params, gin.Param{Key: "id", Value: id}, gin.Param{Key: "projectId", Value: id})
		c.Next()
	}
}
func workspaceMiddleware(h *handlers.Handlers) gin.HandlerFunc {
	return func(c *gin.Context) {
		q := c.Request.URL.Query()
		for _, key := range []string{"projectId", "project"} {
			for _, value := range q[key] {
				if value == "" {
					continue
				}
				if _, err := h.ResolveWorkspaceID(value); err != nil {
					c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "workspace_scope"})
					return
				}
			}
			q.Del(key)
		}
		c.Request.URL.RawQuery = q.Encode()
		if strings.HasPrefix(c.Request.URL.Path, "/api/workspace") {
			bindWorkspace(h)(c)
			return
		}
		c.Next()
	}
}
