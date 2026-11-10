package handlers_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestSoloBrowserDiagnosticsAreOwnedBoundedAndDoNotChangeSession(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, nil)
	for _, row := range []models.SoloSession{
		{ID: "own-diagnostic", UserID: "admin", Status: models.SoloStatusRunning},
		{ID: "other-diagnostic", UserID: "someone-else", Status: models.SoloStatusRunning},
	} {
		if err := hn.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	valid := func() map[string]any {
		return map[string]any{"viewId": strings.Repeat("a", 32), "event": "history_slow", "received": 174, "processed": 0, "buffered": 174, "pendingHistory": 1, "elapsedMs": 5000, "acknowledged": -1}
	}
	if res := hn.do(http.MethodPost, "/api/solo/sessions/own-diagnostic/diagnostics", valid()); res.Code != http.StatusNoContent {
		t.Fatal(res.Code, res.Body)
	}
	for _, id := range []string{"other-diagnostic", "missing"} {
		if res := hn.do(http.MethodPost, "/api/solo/sessions/"+id+"/diagnostics", valid()); res.Code != http.StatusForbidden && res.Code != http.StatusNotFound {
			t.Fatalf("foreign diagnostics accepted: %d", res.Code)
		}
	}
	for _, kind := range []string{"view", "event", "negative", "large"} {
		body := valid()
		switch kind {
		case "view":
			body["viewId"] = "private arbitrary text"
		case "event":
			body["event"] = "private arbitrary text"
		case "negative":
			body["received"] = -2
		case "large":
			body["extra"] = strings.Repeat("x", 4096)
		}
		if res := hn.do(http.MethodPost, "/api/solo/sessions/own-diagnostic/diagnostics", body); res.Code != http.StatusBadRequest {
			t.Fatalf("invalid %s accepted: %d", kind, res.Code)
		}
	}
	var row models.SoloSession
	if err := hn.db.First(&row, "id = ?", "own-diagnostic").Error; err != nil || row.Status != models.SoloStatusRunning || row.SandboxID != 0 {
		t.Fatal("diagnostics changed business state", row, err)
	}
}
