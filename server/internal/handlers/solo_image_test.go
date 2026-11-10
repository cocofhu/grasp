package handlers_test

import (
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestSoloImagesRequireSessionOwnershipAndDoNotCacheFailures(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, nil, nil)
	for _, row := range []models.SoloSession{
		{ID: "own-image", UserID: "admin", Status: models.SoloStatusRunning},
		{ID: "other-image", UserID: "other", Status: models.SoloStatusRunning},
	} {
		if err := hn.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/api/solo/sessions/own-image/images?path=/tmp/full.png", http.StatusNotFound},
		{"/api/solo/sessions/own-image/images", http.StatusBadRequest},
		{"/api/solo/sessions/other-image/images?path=/tmp/full.png", http.StatusNotFound},
	} {
		response := hn.do(http.MethodGet, tc.path, nil)
		if response.Code != tc.status || response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: %d %s", tc.path, response.Code, response.Body)
		}
	}
	if response := hn.doPublic(http.MethodGet, "/api/solo/sessions/own-image/images?path=/tmp/full.png", nil, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated image: %d %s", response.Code, response.Body)
	}
}
