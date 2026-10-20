package router

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/handlers"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
)

func TestWorkspaceBoundaryBindsOnlyCanonicalIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	h := &handlers.Handlers{Projects: services.NewProjectService(db)}
	r := gin.New()
	r.Use(workspaceMiddleware(h))
	r.GET("/api/workspace", func(c *gin.Context) { c.JSON(200, gin.H{"id": c.Param("id"), "query": c.Request.URL.RawQuery}) })
	r.GET("/api/health", func(c *gin.Context) { c.Status(200) })
	for _, tc := range []struct {
		path string
		code int
	}{
		{"/api/workspace", 200},
		{"/api/workspace?projectId=" + models.DefaultProjectID + "&project=" + models.DefaultProjectID, 200},
		{"/api/workspace?projectId=foreign", 400},
		{"/api/workspace?projectId=" + models.DefaultProjectID + "&projectId=foreign", 400},
		{"/api/health", 200},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.code {
			t.Fatalf("%s = %d %s", tc.path, w.Code, w.Body)
		}
		if tc.code == 200 && strings.HasPrefix(tc.path, "/api/workspace") && (!strings.Contains(w.Body.String(), models.DefaultProjectID) || !strings.Contains(w.Body.String(), `"query":""`)) {
			t.Fatalf("canonical binding = %s", w.Body)
		}
	}
	if err = db.Create(&models.Project{ID: "foreign", Name: "Foreign"}).Error; err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/workspace", nil))
	if w.Code != 409 {
		t.Fatalf("ambiguous workspace = %d", w.Code)
	}
	if isDrainBlocked("POST", "/api/workspace/maintenance") || !isDrainBlocked("POST", "/api/workflows") {
		t.Fatal("maintenance must remain available while normal mutation admission is stopped")
	}
}
