package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestWorkflowChatRouteUsesOnlyCallerOwnedSolo(t *testing.T) {
	h := newHarness(t)
	h.h.Solo = services.NewSoloService(h.db, nil, nil, services.NewProjectService(h.db))
	if err := h.db.Create(&models.SoloSession{ID: "private-origin", UserID: "alice", Title: "hidden title", Status: models.SoloStatusStopped}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&models.SoloMessage{ID: "private-text", SessionID: "private-origin", Role: "user", Content: "private content"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Create(&models.Run{ID: "historic", OriginSoloID: "private-origin", CreatedBy: "alice", Title: "workspace-visible Run", Status: "completed"}).Error; err != nil {
		t.Fatal(err)
	}
	response := h.do(http.MethodGet, "/api/runs/historic/chat", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	var value struct {
		SoloID string `json:"soloId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value.SoloID != "" || strings.Contains(response.Body.String(), "private") {
		t.Fatalf("private origin leaked: %s", response.Body.String())
	}
	var sessions, sandboxes int64
	h.db.Model(&models.SoloSession{}).Count(&sessions)
	h.db.Model(&models.Sandbox{}).Count(&sandboxes)
	if sessions != 1 || sandboxes != 0 {
		t.Fatal("passive history GET created resources")
	}
	if err := h.db.Model(&models.SoloSession{}).Where("id = ?", "private-origin").Update("user_id", "admin").Error; err != nil {
		t.Fatal(err)
	}
	response = h.do(http.MethodGet, "/api/runs/historic/chat", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"soloId":"private-origin"`) {
		t.Fatalf("owner: %d %s", response.Code, response.Body.String())
	}
	if response = h.do(http.MethodGet, "/api/runs/missing/chat", nil); response.Code != http.StatusNotFound {
		t.Fatalf("missing %d", response.Code)
	}
}
