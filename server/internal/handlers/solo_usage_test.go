package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestListSoloSessionsReportsWorkspaceWideUsage(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, nil)
	for _, row := range []models.Sandbox{
		{Name: "own-usage", Purpose: services.SandboxPurposeSolo, Status: "running", ThreadID: "own-usage"},
		{Name: "other-usage", Purpose: services.SandboxPurposeSolo, Status: "running", ThreadID: "other-usage"},
	} {
		if err := hn.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []models.SoloSession{
		{ID: "own-usage", UserID: "admin", Status: models.SoloStatusRunning},
		{ID: "other-usage", UserID: "someone-else", Status: models.SoloStatusRunning},
	} {
		if err := hn.db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	res := hn.do(http.MethodGet, "/api/solo/sessions?status=active", nil)
	if res.Code != http.StatusOK {
		t.Fatalf("list: %d %s", res.Code, res.Body.String())
	}
	var body struct {
		Sessions      []json.RawMessage `json:"sessions"`
		MaxSandboxes  int               `json:"maxSandboxes"`
		UsedSandboxes int               `json:"usedSandboxes"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Sessions) != 1 || body.UsedSandboxes != 2 || body.MaxSandboxes <= 0 {
		t.Fatalf("usage must count every owner while listing only the caller: %+v", body)
	}
}
