package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gorilla/websocket"
)

func TestSoloEmbedBindsToCurrentOwnedSandbox(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, nil)
	hn.h.Preview = services.NewPreviewService(hn.db, nil)
	hn.h.Preview.SetAgents(hn.h.Agents)
	hn.host.SetPreviewStore(hn.h.Preview)
	hn.host.SetPreviewSandboxOps(hn.h.Preview)
	if err := hn.h.Agents.Save(services.Agent{Name: "solo-preview-agent", AcpBackend: services.AcpBackendCursor, Capabilities: &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Tools: []string{models.ToolSetPreview},
	}}); err != nil {
		t.Fatal(err)
	}
	const runID, sessionID = "solo-embed-run", "solo-embed-session"
	sb := models.Sandbox{Name: "solo-embed-sandbox", RunID: runID, ThreadID: sessionID, Profile: "solo-preview-agent", Purpose: services.SandboxPurposeSolo, Status: "running"}
	if err := hn.db.Create(&sb).Error; err != nil {
		t.Fatal(err)
	}
	row := models.SoloSession{ID: sessionID, UserID: "admin", AgentName: sb.Profile, SandboxID: sb.ID, Status: models.SoloStatusRunning}
	if err := hn.db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := hn.db.Create(&models.SoloMessage{ID: "history", SessionID: sessionID, Role: "user", Content: "existing Solo history"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := hn.h.Preview.UpsertPreviewPort(mcp.PreviewPort{
		RunID: runID, NodeID: "solo", Port: 8899, Host: strings.TrimRight(embedDirectURL, "/"), Healthy: true,
	}); err != nil {
		t.Fatal(err)
	}
	ticketPath := "/api/solo/sessions/" + sessionID + "/embed-ticket"
	if err := hn.db.Model(&row).Update("user_id", "other-user").Error; err != nil {
		t.Fatal(err)
	}
	if w := hn.do(http.MethodPost, ticketPath, nil); w.Code != http.StatusNotFound {
		t.Fatalf("another user's ticket: %d %s", w.Code, w.Body)
	}
	if err := hn.db.Model(&row).Update("user_id", "admin").Error; err != nil {
		t.Fatal(err)
	}
	issued := hn.do(http.MethodPost, ticketPath, nil)
	if issued.Code != http.StatusOK {
		t.Fatalf("ticket: %d %s", issued.Code, issued.Body)
	}
	ticket, _ := parseJSON(t, issued)["ticket"].(string)
	runToken := hn.host.RegisterRun(runID)
	origin := hn.doEmbed(http.MethodGet, "/mcp/runs/"+runID+"/embed-origin?nodeId=solo&ticket="+ticket, nil, bearerHeader(runToken))
	if origin.Code != http.StatusOK {
		t.Fatalf("injector origin: %d %s", origin.Code, origin.Body)
	}
	claims := redeem(t, hn, ticket)
	token, _ := claims["token"].(string)
	if claims["kind"] != models.EmbedKindSolo || claims["runId"] != runID || token == "" {
		t.Fatalf("wrong claims: kind=%v run=%v", claims["kind"], claims["runId"])
	}
	path := "/embed-api/solo/" + runID
	if w := hn.doEmbed(http.MethodGet, path+"/messages", nil, bearerHeader(token)); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "existing Solo history") {
		t.Fatalf("history: %d %s", w.Code, w.Body)
	}
	if w := hn.doEmbed(http.MethodGet, "/embed-api/solo/another-run/messages", nil, bearerHeader(token)); w.Code != http.StatusUnauthorized {
		t.Fatalf("another run: %d", w.Code)
	}

	server := httptest.NewServer(hn.r)
	defer server.Close()
	dialer := websocket.Dialer{Subprotocols: []string{"grasp-solo-embed", token}}
	socket, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+path+"/chat", http.Header{"Origin": []string{server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = socket.Close() }()
	_ = socket.SetReadDeadline(time.Now().Add(3 * time.Second))
	var frame map[string]any
	if err := socket.ReadJSON(&frame); err != nil || frame["event"] != "queue_state" || socket.Subprotocol() != "grasp-solo-embed" {
		t.Fatalf("initial socket frame: event=%v err=%v", frame["event"], err)
	}

	if err := hn.db.Model(&row).Update("sandbox_id", sb.ID+1).Error; err != nil {
		t.Fatal(err)
	}
	if w := hn.doEmbed(http.MethodGet, path+"/messages", nil, bearerHeader(token)); w.Code != http.StatusUnauthorized {
		t.Fatalf("superseded sandbox: %d", w.Code)
	}
}
