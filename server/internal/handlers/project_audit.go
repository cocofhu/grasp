package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

// auditActorFromContext returns the Session username or system+unattributable.
func (h *Handlers) auditActorFromContext(c *gin.Context) services.AuditActor {
	if h.Auth == nil {
		return services.SystemActor()
	}
	sess, ok := auth.GetSession(c)
	if !ok || sess == nil || strings.TrimSpace(sess.Username) == "" {
		return services.SystemActor()
	}
	return services.ActorFromUsername(sess.Username)
}

// canViewProjectAudit reports whether the caller may list/export project audit.
// Product rule: is_admin OR users who can manage project config (UpdateProject).
// Today any authenticated user can UpdateProject; tests may override via
// CanViewProjectAudit to simulate a read-only denial.
func (h *Handlers) canViewProjectAudit(c *gin.Context, projectID string) bool {
	if h.CanViewProjectAudit != nil {
		username := ""
		if sess, ok := auth.GetSession(c); ok && sess != nil {
			username = sess.Username
		}
		return h.CanViewProjectAudit(username, projectID)
	}
	if h.Auth == nil {
		return true
	}
	sess, ok := auth.GetSession(c)
	if !ok || sess == nil {
		return false
	}
	if h.Auth.IsAdmin(sess.Username) {
		return true
	}
	// Equivalent to "can UpdateProject": any authenticated user today.
	return true
}

func (h *Handlers) requireProjectAuditAccess(c *gin.Context, projectID string) bool {
	if h.Projects == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "projects unavailable"})
		return false
	}
	if _, ok := h.Projects.Get(projectID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return false
	}
	if !h.canViewProjectAudit(c, projectID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden: cannot view project audit"})
		return false
	}
	return true
}

func parseAuditTimeWindow(raw string) (from *time.Time, to *time.Time) {
	now := time.Now()
	switch strings.TrimSpace(raw) {
	case "", "24h":
		t := now.Add(-24 * time.Hour)
		return &t, nil
	case "7d":
		t := now.Add(-7 * 24 * time.Hour)
		return &t, nil
	case "30d":
		t := now.Add(-30 * 24 * time.Hour)
		return &t, nil
	case "all":
		return nil, nil
	default:
		t := now.Add(-24 * time.Hour)
		return &t, nil
	}
}

func (h *Handlers) parseAuditListFilter(c *gin.Context, projectID string) (services.AuditListFilter, bool) {
	from, to := parseAuditTimeWindow(c.DefaultQuery("time", "24h"))
	// Optional explicit from/to override window presets.
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from (RFC3339)"})
			return services.AuditListFilter{}, false
		}
		from = &t
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid to (RFC3339)"})
			return services.AuditListFilter{}, false
		}
		to = &t
	}
	pg, ok := parsePagination(c)
	if !ok {
		return services.AuditListFilter{}, false
	}
	return services.AuditListFilter{
		ProjectID:  projectID,
		From:       from,
		To:         to,
		Actor:      c.Query("actor"),
		CallerKind: c.Query("callerKind"),
		Action:     c.Query("action"),
		Resource:   c.Query("resource"),
		RunID:      c.Query("runId"),
		NodeID:     c.Query("nodeId"),
		Search:     c.Query("search"),
		Page:       pg.Page,
		PageSize:   pg.PageSize,
	}, true
}

func auditEventDTO(ev models.ProjectAuditEvent) gin.H {
	resource := ev.ResourceType
	if ev.ResourceID != "" {
		if resource != "" {
			resource += "/" + ev.ResourceID
		} else {
			resource = ev.ResourceID
		}
	}
	return gin.H{
		"id":             ev.ID,
		"projectId":      ev.ProjectID,
		"occurredAt":     ev.OccurredAt,
		"actor":          ev.Actor,
		"unattributable": ev.Unattributable,
		"callerKind":     ev.CallerKind,
		"action":         ev.Action,
		"resourceType":   ev.ResourceType,
		"resourceId":     ev.ResourceID,
		"resource":       resource,
		"runId":          ev.RunID,
		"nodeId":         ev.NodeID,
		"outcome":        ev.Outcome,
		"summary":        ev.Summary,
		"payload":        ev.Payload,
	}
}

// parseAuditFacetsFilter parses time (+ optional runId / from / to) for facets.
// Action-namespace cascade is no longer used.
func (h *Handlers) parseAuditFacetsFilter(c *gin.Context, projectID string) (services.AuditListFilter, bool) {
	from, to := parseAuditTimeWindow(c.DefaultQuery("time", "24h"))
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid from (RFC3339)"})
			return services.AuditListFilter{}, false
		}
		from = &t
	}
	if raw := strings.TrimSpace(c.Query("to")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid to (RFC3339)"})
			return services.AuditListFilter{}, false
		}
		to = &t
	}
	return services.AuditListFilter{
		ProjectID: projectID,
		From:      from,
		To:        to,
		RunID:     c.Query("runId"),
	}, true
}

// ListProjectAuditFacets returns Run / node / resource options for dual-mode filters.
// GET /api/projects/:id/audit/facets
func (h *Handlers) ListProjectAuditFacets(c *gin.Context) {
	if h.Audit == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "audit unavailable"})
		return
	}
	projectID := c.Param("id")
	if !h.requireProjectAuditAccess(c, projectID) {
		return
	}
	f, ok := h.parseAuditFacetsFilter(c, projectID)
	if !ok {
		return
	}
	facets, err := h.Audit.ListFacets(f)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, facets)
}

// ListProjectAudit returns a paginated project audit timeline.
// GET /api/projects/:id/audit
func (h *Handlers) ListProjectAudit(c *gin.Context) {
	if h.Audit == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "audit unavailable"})
		return
	}
	projectID := c.Param("id")
	if !h.requireProjectAuditAccess(c, projectID) {
		return
	}
	f, ok := h.parseAuditListFilter(c, projectID)
	if !ok {
		return
	}
	items, total, err := h.Audit.ListPage(f)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	stats, err := h.Audit.CountStats(f)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	out := make([]gin.H, 0, len(items))
	for _, ev := range items {
		out = append(out, auditEventDTO(ev))
	}
	resp := paginatedResponse(out, int(total), f.Page, f.PageSize)
	resp["stats"] = gin.H{
		"total": stats.Total,
		"mcp":   stats.MCP,
		"fail":  stats.Fail,
	}
	c.JSON(http.StatusOK, resp)
}

// ExportProjectAudit exports matching audit events as JSON or plain text and
// records an audit.export meta-event on success.
// GET /api/projects/:id/audit/export?format=json|text
func (h *Handlers) ExportProjectAudit(c *gin.Context) {
	if h.Audit == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "audit unavailable"})
		return
	}
	projectID := c.Param("id")
	if !h.requireProjectAuditAccess(c, projectID) {
		return
	}
	format := strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", "json")))
	if format != "json" && format != "text" && format != "txt" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "format must be json or text"})
		return
	}
	if format == "txt" {
		format = "text"
	}
	f, ok := h.parseAuditListFilter(c, projectID)
	if !ok {
		return
	}
	items, err := h.Audit.ListAllMatching(f, 5000)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	filename := fmt.Sprintf("project-%s-audit.%s", sanitizeFilename(projectID), map[string]string{"json": "json", "text": "txt"}[format])
	var body []byte
	var contentType string
	if format == "json" {
		dtos := make([]gin.H, 0, len(items))
		for _, ev := range items {
			dtos = append(dtos, auditEventDTO(ev))
		}
		raw, err := json.MarshalIndent(dtos, "", "  ")
		if err != nil {
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "export marshal failed"})
			return
		}
		body = raw
		contentType = "application/json; charset=utf-8"
	} else {
		body = []byte(services.FormatAuditText(items))
		contentType = "text/plain; charset=utf-8"
	}

	// Meta-audit only after export bytes are successfully generated.
	actor := h.auditActorFromContext(c)
	filterSummary := map[string]any{
		"time":       c.DefaultQuery("time", "24h"),
		"actor":      f.Actor,
		"callerKind": f.CallerKind,
		"action":     f.Action,
		"resource":   f.Resource,
		"runId":      f.RunID,
		"nodeId":     f.NodeID,
		"search":     f.Search,
		"from":       f.From,
		"to":         f.To,
	}
	h.Audit.Record(services.AuditRecord{
		ProjectID:    projectID,
		Actor:        actor,
		Action:       models.AuditActionAuditExport,
		ResourceType: "audit",
		ResourceID:   "export",
		Outcome:      models.AuditOutcomeOK,
		Summary:      fmt.Sprintf("export %s · %d events", format, len(items)),
		Payload: map[string]any{
			"format":  format,
			"filters": filterSummary,
			"count":   len(items),
		},
	})

	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Data(http.StatusOK, contentType, body)
}

// recordAudit is a nil-safe helper for write-path instrumentation.
func (h *Handlers) recordAudit(rec services.AuditRecord) {
	if h == nil || h.Audit == nil {
		return
	}
	h.Audit.Record(rec)
}
