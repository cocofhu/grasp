package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParsePaginationDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/?page=2", nil)
	pg, ok := parsePagination(c)
	if !ok || pg.Page != 2 || pg.PageSize != defaultPageSize {
		t.Fatalf("pg=%+v ok=%v", pg, ok)
	}
	c, _ = gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	pg, ok = parsePagination(c)
	if !ok || pg.Page != 1 || pg.PageSize != defaultPageSize {
		t.Fatalf("no params pg=%+v ok=%v", pg, ok)
	}
}

func TestPageHasMoreEdge(t *testing.T) {
	if pageHasMore(1, 20, 20) {
		t.Fatal("exact page should not have more")
	}
	if !pageHasMore(1, 10, 25) {
		t.Fatal("should have more")
	}
}
