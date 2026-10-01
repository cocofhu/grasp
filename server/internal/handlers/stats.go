package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

// parseGlobalTokenStatsQuery reads the shared /stats/token filter params.
// Query: window=24h|7d|30d|90d|all (default all), from/to=YYYY-MM-DD (custom
// range, overrides window), granularity=hour|day|week, timezone,
// utcOffsetMinutes, source=all|workflow|pm|studio, status=ok|failed|cancelled,
// projectId, modelKey, workflowId, nodeType, runId.
func parseGlobalTokenStatsQuery(c *gin.Context) (services.GlobalTokenStatsQuery, bool) {
	q := services.GlobalTokenStatsQuery{
		Window:      c.DefaultQuery("window", services.TokenStatsWindowAll),
		From:        strings.TrimSpace(c.Query("from")),
		To:          strings.TrimSpace(c.Query("to")),
		Granularity: strings.TrimSpace(c.Query("granularity")),
		Timezone:    c.Query("timezone"),
		Source:      c.DefaultQuery("source", services.GlobalTokenStatsSourceAll),
		Status:      strings.TrimSpace(c.Query("status")),
		ProjectID:   strings.TrimSpace(c.Query("projectId")),
		ModelKey:    strings.TrimSpace(c.Query("modelKey")),
		WorkflowID:  strings.TrimSpace(c.Query("workflowId")),
		NodeType:    strings.TrimSpace(c.Query("nodeType")),
		RunID:       strings.TrimSpace(c.Query("runId")),
	}
	if raw := strings.TrimSpace(c.Query("utcOffsetMinutes")); raw != "" {
		mins, err := strconv.Atoi(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid utcOffsetMinutes"})
			return q, false
		}
		q.UTCOffsetMinutes = &mins
	}
	return q, true
}

func writeTokenStatsError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrInvalidTokenStatsWindow),
		errors.Is(err, services.ErrInvalidTokenStatsTimezone),
		errors.Is(err, services.ErrInvalidTokenStatsRange):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrTokenStatsTimeout):
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":     err.Error(),
			"retryable": true,
		})
	default:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

// GetGlobalTokenStats returns cross-project token analytics for /stats.
func (h *Handlers) GetGlobalTokenStats(c *gin.Context) {
	if h.Projects == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "projects unavailable"})
		return
	}
	q, ok := parseGlobalTokenStatsQuery(c)
	if !ok {
		return
	}
	result, err := h.Projects.GlobalTokenStats(c.Request.Context(), q)
	if err != nil {
		writeTokenStatsError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// ListTokenUsageEvents pages raw ledger rows (drill-down detail) under the
// same filters as GetGlobalTokenStats. Extra query: page, pageSize, sort.
func (h *Handlers) ListTokenUsageEvents(c *gin.Context) {
	if h.Projects == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "projects unavailable"})
		return
	}
	q, ok := parseGlobalTokenStatsQuery(c)
	if !ok {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("pageSize", "50"))
	result, err := h.Projects.TokenUsageEvents(c.Request.Context(), services.TokenUsageEventsQuery{
		GlobalTokenStatsQuery: q, Page: page, PageSize: size, Sort: c.Query("sort"),
	})
	if err != nil {
		writeTokenStatsError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetTokenPricing returns the model price table used for cost estimation.
func (h *Handlers) GetTokenPricing(c *gin.Context) {
	if h.Projects == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "projects unavailable"})
		return
	}
	c.JSON(http.StatusOK, h.Projects.TokenPricing())
}

// UpdateTokenPricing replaces the model price table (admin only).
func (h *Handlers) UpdateTokenPricing(c *gin.Context) {
	if h.Projects == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "projects unavailable"})
		return
	}
	if !h.requireAdmin(c) {
		return
	}
	var body services.TokenPricing
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	out, err := h.Projects.SaveTokenPricing(body)
	if err != nil {
		if errors.Is(err, services.ErrInvalidTokenPricing) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}
