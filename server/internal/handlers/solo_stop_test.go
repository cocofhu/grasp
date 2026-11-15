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
	if res := hn.do(http.MethodPost, "/api/solo/sessions/stopped/wake", nil); res.Code != http.StatusConflict {
		t.Fatalf("wake stopped: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodPost, "/api/solo/sessions/missing/wake", nil); res.Code != http.StatusNotFound {
		t.Fatalf("wake missing: %d %s", res.Code, res.Body)
	}
}

func TestSoloBackupSelectionRemoved(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, services.NewProjectService(hn.db))
	if err := hn.db.Create(&models.SoloSession{ID: "stopped", UserID: "admin", Status: models.SoloStatusStopped}).Error; err != nil {
		t.Fatal(err)
	}
	if res := hn.do(http.MethodGet, "/api/solo/sessions/stopped/backups", nil); res.Code != http.StatusNotFound {
		t.Fatalf("removed backup listing route: %d %s", res.Code, res.Body)
	}
	for _, body := range []any{map[string]any{"backupId": 9999}, "x"} {
		if res := hn.do(http.MethodPost, "/api/solo/sessions/stopped/resume", body); res.Code != http.StatusBadRequest {
			t.Fatalf("obsolete selection or invalid body accepted: %d %s", res.Code, res.Body)
		}
	}
}
