package handlers_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestSoloRequestRoutesRejectMissingIdentityAndInvalidAttachments(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, services.NewProjectService(hn.db))
	if err := hn.db.Create(&models.SoloSession{ID: "request-chat", UserID: "admin", Status: models.SoloStatusRunning}).Error; err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/solo/creation-requests", "/api/solo/sessions/request-chat/turn-requests"} {
		response := hn.do(http.MethodPost, path, map[string]string{"text": "no stable identity"})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s accepted unstable request: %d %s", path, response.Code, response.Body)
		}
	}
	for _, image := range []models.PromptImage{{Ref: "blob:../../invalid"}, {Data: "invalid-base64!", MimeType: "image/png"}, {MimeType: "image/png"}} {
		response := hn.do(http.MethodPost, "/api/solo/sessions/request-chat/turn-requests", map[string]any{"clientMessageId": "invalid-attachment", "text": "inspect", "images": []models.PromptImage{image}})
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_attachment") {
			t.Fatalf("invalid input prompted an indefinite retry: %d %s", response.Code, response.Body)
		}
	}
	response := hn.do(http.MethodPost, "/api/solo/sessions/request-chat/turn-requests", map[string]any{"clientMessageId": "store-unavailable", "text": "inspect", "images": []models.PromptImage{{Data: "aGVsbG8=", MimeType: "image/png"}}})
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "message_not_confirmed") {
		t.Fatalf("store unavailable was misclassified as client input failure: %d %s", response.Code, response.Body)
	}
	var accepted int64
	hn.db.Model(&models.ChatQueueTurn{}).Count(&accepted)
	if accepted != 0 {
		t.Fatal("rejected requests were accepted for execution")
	}
}
