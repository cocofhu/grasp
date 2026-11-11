package handlers_test

import (
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestSoloStopAndResumeEndpoints(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, services.NewProjectService(hn.db))
	for _, row := range []models.SoloSession{
		{ID: "stopped", UserID: "admin", Status: models.SoloStatusStopped},
		{ID: "closed", UserID: "admin", Status: models.SoloStatusClosed},
	} {
		if err := hn.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if res := hn.do(http.MethodPost, "/api/solo/sessions/stopped/stop", nil); res.Code != http.StatusOK {
		t.Fatalf("stop of a stopped session: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodPost, "/api/solo/sessions/closed/stop", nil); res.Code != http.StatusConflict {
		t.Fatalf("stop closed: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodPost, "/api/solo/sessions/closed/resume", nil); res.Code != http.StatusConflict {
		t.Fatalf("resume closed: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodPost, "/api/solo/sessions/missing/resume", nil); res.Code != http.StatusNotFound {
		t.Fatalf("resume missing: %d %s", res.Code, res.Body)
	}
}
