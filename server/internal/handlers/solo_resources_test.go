package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestSoloResourcesUseCurrentOwnedSandbox(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, nil)
	preview := services.NewPreviewService(hn.db, nil)
	hn.host.SetPreviewStore(preview)
	hn.host.SetSoloArtifactPreviewHook(preview.PinSoloArtifact)

	const runID = "solo-current"
	token := hn.host.RegisterRun(runID)
	hn.host.SetActiveNode(runID, "solo", &models.AgentCapabilities{Tools: []string{models.ToolSetArtifactPreview}})
	sb := models.Sandbox{Name: "current-sandbox", RunID: runID, ThreadID: "current", Profile: "agent", Purpose: services.SandboxPurposeSolo, Status: "running"}
	if err := hn.db.Create(&sb).Error; err != nil {
		t.Fatal(err)
	}
	row := models.SoloSession{ID: sb.ThreadID, UserID: "admin", AgentName: sb.Profile, SandboxID: sb.ID, Status: models.SoloStatusRunning}
	if err := hn.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := hn.host.WriteArtifact(runID, token, "solo", "demo.html", "<p>demo</p>", "html"); err != nil {
		t.Fatal(err)
	}
	if err := preview.UpsertPreviewPort(mcp.PreviewPort{RunID: runID, NodeID: "solo", Port: 8899, Healthy: true}); err != nil {
		t.Fatal(err)
	}
	hn.host.PutPreviewPortForTest(runID, "solo", 5173, "second app")

	pin := func() bool {
		t.Helper()
		_, body := hn.host.ServeRPC(runID, token, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"set_artifact_preview","arguments":{"name":"demo.html"}}}`))
		var response struct {
			Result *struct {
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &response); err != nil || response.Result == nil {
			t.Fatalf("pin response: %s (%v)", body, err)
		}
		return !response.Result.IsError
	}
	if !pin() || !pin() {
		t.Fatal("pinning an existing Solo artifact should be idempotent")
	}

	path := "/api/solo/sessions/current/resources"
	var resources struct {
		RunID           string
		PreviewArtifact string
		Artifacts       []models.Artifact
		Ports           []mcp.PreviewPort
	}
	res := hn.do(http.MethodGet, path, nil)
	if err := json.Unmarshal(res.Body.Bytes(), &resources); err != nil || res.Code != http.StatusOK ||
		resources.RunID != runID || resources.PreviewArtifact != "demo.html" ||
		len(resources.Artifacts) != 1 || resources.Artifacts[0].Name != "demo.html" ||
		len(resources.Ports) != 2 || resources.Ports[0].Port != 5173 || resources.Ports[1].Port != 8899 {
		t.Fatalf("resources: %d %s (%v)", res.Code, res.Body, err)
	}

	next := models.Sandbox{Name: "next-sandbox", RunID: "solo-next", ThreadID: row.ID, Profile: row.AgentName, Purpose: services.SandboxPurposeSolo, Status: "running"}
	if err := hn.db.Create(&next).Error; err != nil {
		t.Fatal(err)
	}
	if err := hn.db.Model(&row).Update("sandbox_id", next.ID).Error; err != nil {
		t.Fatal(err)
	}
	if pin() {
		t.Fatal("a superseded sandbox must not pin an artifact")
	}
	if _, err := hn.h.Arts.SaveFile(context.Background(), runID, "solo", "retained.txt", row.ID, strings.NewReader("retained file")); err != nil {
		t.Fatal(err)
	}
	res = hn.do(http.MethodGet, path, nil)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "solo-next") || strings.Contains(res.Body.String(), "demo.html") || !strings.Contains(res.Body.String(), "retained.txt") {
		t.Fatalf("resources after replacement: %d %s", res.Code, res.Body)
	}

	if err := hn.db.Model(&row).Update("user_id", "other-user").Error; err != nil {
		t.Fatal(err)
	}
	if res := hn.do(http.MethodGet, path, nil); res.Code != http.StatusNotFound {
		t.Fatalf("another user's resources: %d %s", res.Code, res.Body)
	}
}
