package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

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

func TestSoloBackupEndpoints(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, services.NewProjectService(hn.db))
	if err := hn.db.Create(&models.SoloSession{ID: "stopped", UserID: "admin", Status: models.SoloStatusStopped}).Error; err != nil {
		t.Fatal(err)
	}
	older := time.Now().Add(-time.Hour)
	for _, b := range []models.SoloBackup{
		{SessionID: "stopped", ObjectKey: "solo/stopped/old.tar.gz", Size: 1, Reason: models.SoloBackupReasonScheduled, CreatedAt: older},
		{SessionID: "stopped", ObjectKey: "solo/stopped/new.tar.gz", Size: 2, Reason: models.SoloBackupReasonStop},
	} {
		if err := hn.db.Create(&b).Error; err != nil {
			t.Fatal(err)
		}
	}
	res := hn.do(http.MethodGet, "/api/solo/sessions/stopped/backups", nil)
	var list []map[string]any
	if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &list) != nil || len(list) != 2 || list[0]["reason"] != "stop" {
		t.Fatalf("list backups: %d %s", res.Code, res.Body)
	}
	if _, leaked := list[0]["objectKey"]; leaked {
		t.Fatal("storage key exposed")
	}
	if res := hn.do(http.MethodGet, "/api/solo/sessions/missing/backups", nil); res.Code != http.StatusNotFound {
		t.Fatalf("list backups of a missing session: %d", res.Code)
	}
	if res := hn.do(http.MethodPost, "/api/solo/sessions/stopped/resume", map[string]any{"backupId": 9999}); res.Code != http.StatusNotFound {
		t.Fatalf("resume from an unknown backup: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodPost, "/api/solo/sessions/stopped/resume", "x"); res.Code != http.StatusBadRequest {
		t.Fatalf("resume with a malformed body: %d %s", res.Code, res.Body)
	}
}
