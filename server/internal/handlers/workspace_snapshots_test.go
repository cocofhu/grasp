package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/database"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestWorkspaceSnapshotHistoryRequiresCookieAndHidesPrivateReferences(t *testing.T) {
	db, err := database.OpenSQLiteTest(filepath.Join(t.TempDir(), "snapshots.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(db) })
	if err := db.AutoMigrate(&models.WorkspaceSnapshot{}, &models.WorkspaceSnapshotJob{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("workspace_snapshots").Create(map[string]any{"id": "snapshot-original", "scope_kind": "solo", "scope_id": "solo-original", "actor_id": "previous-owner", "state": "ready", "archive_ref": "private-archive", "manifest_ref": "private-manifest", "lineage": "private-lineage", "created_at": time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}
	calls := 0
	catalog, err := services.NewWorkspaceSnapshotCatalog(db, func(_ context.Context, _ *gorm.DB, actor, kind, id string) error {
		calls++
		if actor != "alice" || kind != "solo" || id != "solo-original" {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{Auth: &auth.Service{}, WorkspaceSnapshots: catalog}
	request := func(cookie, embedded bool, id string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/solo/sessions/solo-original/workspace-snapshots/snapshot-original?actorId=forged", nil)
		c.Params = gin.Params{{Key: "id", Value: id}, {Key: "snapshotId", Value: "snapshot-original"}}
		if cookie {
			c.Set("auth_session", &models.Session{Username: "alice"})
		}
		if embedded {
			c.Set(soloEmbedUserKey, "alice")
		}
		h.GetSoloWorkspaceSnapshot(c)
		return w
	}
	if w := request(false, false, "solo-original"); w.Code != http.StatusUnauthorized || calls != 0 {
		t.Fatal("no cookie reached metadata authority", w.Code, calls)
	}
	if w := request(true, true, "solo-original"); w.Code != http.StatusForbidden || calls != 0 {
		t.Fatal("embedded authority became workspace history authority", w.Code, calls)
	}
	if w := request(true, false, "foreign-solo"); w.Code != http.StatusNotFound || calls != 1 {
		t.Fatal("route scope was not enforced", w.Code, calls)
	}
	w := request(true, false, "solo-original")
	if w.Code != http.StatusOK || calls != 2 || !strings.Contains(w.Body.String(), "snapshot-original") || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "previous-owner") {
		t.Fatal("metadata projection exposed private source credentials", w.Code, w.Body.String(), calls)
	}
}
