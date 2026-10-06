package apikey_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/auth/apikey"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"

	"github.com/gin-gonic/gin"
)

func TestMiddlewareUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "apikey.db"))
	if err != nil {
		t.Fatal(err)
	}
	svc := services.NewAPIKeyService(db)

	r := gin.New()
	r.GET("/v1/ping", apikey.Middleware(svc), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	cases := []struct {
		name   string
		header string
	}{
		{"missing", ""},
		{"basic", "Basic abc"},
		{"empty bearer", "Bearer "},
		{"bad key", "Bearer cf_wf_notreal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/v1/ping", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			r.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestMiddlewareAndWorkflowID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "apikey.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.WorkflowDef{ProjectID: models.DefaultProjectID, ID: "wf-1", Name: "W", Version: 1, PublishedVersion: 1}).Error; err != nil {
		t.Fatal(err)
	}
	svc := services.NewAPIKeyService(db)
	res, err := svc.Create("wf-1", "ci")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	r := gin.New()
	r.GET("/v1/ping", apikey.Middleware(svc), func(c *gin.Context) {
		wfID, ok := apikey.WorkflowID(c)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no workflow"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"workflow_id": wfID})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+res.Plaintext)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "wf-1") {
		t.Fatalf("body = %s", w.Body.String())
	}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if _, ok := apikey.WorkflowID(c); ok {
		t.Fatal("WorkflowID should be false without middleware")
	}
}
