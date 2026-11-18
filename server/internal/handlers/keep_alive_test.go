package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestSoloKeepAliveEndpoint(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, services.NewProjectService(hn.db))
	for _, row := range []models.SoloSession{
		{ID: "live", UserID: "admin", Status: models.SoloStatusStopped},
		{ID: "closed", UserID: "admin", Status: models.SoloStatusClosed},
		{ID: "other", UserID: "bob", Status: models.SoloStatusRunning},
	} {
		if err := hn.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	res := hn.do(http.MethodPut, "/api/solo/sessions/live/keep-alive", map[string]bool{"keepAlive": true})
	var view struct {
		KeepAlive bool `json:"keepAlive"`
	}
	if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &view) != nil || !view.KeepAlive {
		t.Fatalf("enable: %d %s", res.Code, res.Body)
	}
	var row models.SoloSession
	hn.db.First(&row, "id = ?", "live")
	if !row.KeepAlive || row.Status != models.SoloStatusStopped {
		t.Fatalf("policy must persist without waking the environment: %+v", row)
	}
	if res := hn.do(http.MethodPut, "/api/solo/sessions/live/keep-alive", map[string]any{}); res.Code != http.StatusBadRequest {
		t.Fatalf("missing value: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodPut, "/api/solo/sessions/closed/keep-alive", map[string]bool{"keepAlive": true}); res.Code != http.StatusConflict {
		t.Fatalf("closed: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodPut, "/api/solo/sessions/other/keep-alive", map[string]bool{"keepAlive": true}); res.Code != http.StatusNotFound {
		t.Fatalf("other user's session: %d %s", res.Code, res.Body)
	}
}

func TestRunKeepAliveEndpoint(t *testing.T) {
	hn := newHarness(t)
	if err := hn.db.Create(&models.Run{ID: "run-1", Status: "waiting_human"}).Error; err != nil {
		t.Fatal(err)
	}
	if res := hn.do(http.MethodPut, "/api/runs/run-1/keep-alive", map[string]bool{"keepAlive": true}); res.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", res.Code, res.Body)
	}
	var run models.Run
	hn.db.First(&run, "id = ?", "run-1")
	if !run.KeepAlive {
		t.Fatal("run keep-alive not persisted")
	}
	res := hn.do(http.MethodGet, "/api/runs?status=waiting_human", nil)
	var list struct {
		Items []struct {
			KeepAlive bool `json:"keepAlive"`
		} `json:"items"`
	}
	if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &list) != nil || len(list.Items) != 1 || !list.Items[0].KeepAlive {
		t.Fatalf("list must expose keepAlive: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodPut, "/api/runs/run-1/keep-alive", map[string]bool{"keepAlive": false}); res.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodPut, "/api/runs/run-1/keep-alive", "x"); res.Code != http.StatusBadRequest {
		t.Fatalf("invalid body: %d %s", res.Code, res.Body)
	}
	if res := hn.do(http.MethodPut, "/api/runs/missing/keep-alive", map[string]bool{"keepAlive": true}); res.Code != http.StatusNotFound {
		t.Fatalf("missing run: %d %s", res.Code, res.Body)
	}
}
