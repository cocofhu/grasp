package handlers

import (
	"net/http"
	"reflect"
	"strconv"

	"github.com/cocofhu/grasp/internal/models"

	"github.com/gin-gonic/gin"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	defaultLimit    = 20
	maxLimit        = 100
)

// pageParams holds validated page/pageSize pagination inputs.
type pageParams struct {
	Page     int
	PageSize int
}

// cursorParams holds validated cursor/limit pagination inputs.
type cursorParams struct {
	Active bool
	Cursor string
	Limit  int
}

// parsePagination reads page/pageSize query params, defaulting to page 1 and
// defaultPageSize. Returns false and writes a 400 when pageSize exceeds maxPageSize.
func parsePagination(c *gin.Context) (pageParams, bool) {
	pageQ := c.Query("page")
	sizeQ := c.Query("pageSize")

	page := 1
	if pageQ != "" {
		v, err := strconv.Atoi(pageQ)
		if err != nil || v < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "page must be a positive integer"})
			return pageParams{}, false
		}
		page = v
	}

	pageSize := defaultPageSize
	if sizeQ != "" {
		v, err := strconv.Atoi(sizeQ)
		if err != nil || v < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "pageSize must be a positive integer"})
			return pageParams{}, false
		}
		if v > maxPageSize {
			c.JSON(http.StatusBadRequest, gin.H{"error": "pageSize must be ≤ 100"})
			return pageParams{}, false
		}
		pageSize = v
	}

	return pageParams{Page: page, PageSize: pageSize}, true
}

// parseCursorPagination reads optional cursor/limit query params. Active when
// either is present. Returns false and writes a 400 when limit exceeds maxLimit.
func parseCursorPagination(c *gin.Context) (cursorParams, bool) {
	cursorQ := c.Query("cursor")
	limitQ := c.Query("limit")
	if cursorQ == "" && limitQ == "" {
		return cursorParams{}, true
	}

	limit := defaultLimit
	if limitQ != "" {
		v, err := strconv.Atoi(limitQ)
		if err != nil || v < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be a positive integer"})
			return cursorParams{}, false
		}
		if v > maxLimit {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be ≤ 100"})
			return cursorParams{}, false
		}
		limit = v
	}

	return cursorParams{Active: true, Cursor: cursorQ, Limit: limit}, true
}

func pageHasMore(page, pageSize, total int) bool {
	return page*pageSize < total
}

func paginatedResponse(items any, total, page, pageSize int) gin.H {
	if items == nil {
		items = []any{}
	} else if v := reflect.ValueOf(items); v.Kind() == reflect.Slice && v.IsNil() {
		items = reflect.MakeSlice(v.Type(), 0, 0).Interface()
	}
	return gin.H{
		"items":    items,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
		"hasMore":  pageHasMore(page, pageSize, total),
	}
}

// pagePersistedEvents slices a persisted event array for cursor/limit pagination.
// Events are chronological; the first page returns the most recent limit items.
func pagePersistedEvents(events []models.AcpEvent, cursor string, limit int) ([]models.AcpEvent, string, bool) {
	total := len(events)
	if total == 0 {
		return nil, "", false
	}
	if cursor == "" {
		start := total - limit
		if start < 0 {
			start = 0
		}
		page := events[start:]
		return page, strconv.Itoa(start), start > 0
	}
	end, err := strconv.Atoi(cursor)
	if err != nil || end <= 0 {
		return nil, "", false
	}
	if end > total {
		end = total
	}
	start := end - limit
	if start < 0 {
		start = 0
	}
	page := events[start:end]
	return page, strconv.Itoa(start), start > 0
}
