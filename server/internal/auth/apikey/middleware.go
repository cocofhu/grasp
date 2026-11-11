// Package apikey provides Bearer API Key authentication for /v1/* routes.
package apikey

import (
	"net/http"
	"strings"

	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

// ContextKey is the Gin context key for the workflow ID bound to the API key.
const ContextKey = "api_key_workflow_id"

// Middleware validates Authorization: Bearer {key} and injects workflow_id.
// Auth failures always return 401 without leaking whether the key exists.
func Middleware(svc *services.APIKeyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("Authorization")
		if !strings.HasPrefix(strings.ToLower(raw), "bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}
		token := strings.TrimSpace(raw[7:])
		wfID, ok := svc.ValidateBearer(token)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}
		c.Set(ContextKey, wfID)
		c.Next()
	}
}

// WorkflowID returns the workflow ID from context (set by Middleware).
func WorkflowID(c *gin.Context) (string, bool) {
	v, ok := c.Get(ContextKey)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok && s != ""
}
