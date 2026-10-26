package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/sandbox"
	"github.com/cocofhu/grasp/internal/sandbox/sandboxtest"
)

func TestPreviewServiceSoloLookupAndPermissions(t *testing.T) {
	for _, tc := range []struct {
		name        string
		nodeID      string
		change      func(*models.Sandbox, *models.SoloSession, *Agent)
		noSession   bool
		noAgent     bool
		noCatalog   bool
		wantLookup  bool
		wantDirect  bool
		wantBinding bool
	}{
		{name: "legacy empty node", wantLookup: true, wantDirect: true, wantBinding: true},
		{name: "explicit Solo node", change: func(sb *models.Sandbox, _ *models.SoloSession, _ *Agent) { sb.NodeID = "solo" }, wantLookup: true, wantDirect: true, wantBinding: true},
		{name: "wrong requested node", nodeID: "other", wantBinding: true},
		{name: "wrong stored node", change: func(sb *models.Sandbox, _ *models.SoloSession, _ *Agent) { sb.NodeID = "other" }},
		{name: "unnamed sandbox", change: func(sb *models.Sandbox, _ *models.SoloSession, _ *Agent) { sb.Name = "" }},
		{name: "missing session", noSession: true},
		{name: "previous restart generation", change: func(_ *models.Sandbox, session *models.SoloSession, _ *Agent) { session.SandboxID++ }},
		{name: "wrong session", change: func(sb *models.Sandbox, _ *models.SoloSession, _ *Agent) { sb.ThreadID = "other-session" }},
		{name: "wrong agent", change: func(_ *models.Sandbox, session *models.SoloSession, _ *Agent) { session.AgentName = "other-agent" }},
		{name: "closed session", change: func(_ *models.Sandbox, session *models.SoloSession, _ *Agent) {
			session.Status = models.SoloStatusClosed
		}},
		{name: "starting session", change: func(_ *models.Sandbox, session *models.SoloSession, _ *Agent) {
			session.Status = models.SoloStatusStarting
		}},
		{name: "failed session", change: func(_ *models.Sandbox, session *models.SoloSession, _ *Agent) {
			session.Status = models.SoloStatusError
		}},
		{name: "wrong purpose", change: func(sb *models.Sandbox, _ *models.SoloSession, _ *Agent) { sb.Purpose = "test" }},
		{name: "no preview grant", change: func(_ *models.Sandbox, _ *models.SoloSession, agent *Agent) { agent.Capabilities.Tools = nil }, wantLookup: true, wantBinding: true},
		{name: "nil capabilities", change: func(_ *models.Sandbox, _ *models.SoloSession, agent *Agent) { agent.Capabilities = nil }, wantLookup: true, wantBinding: true},
		{name: "deleted agent", noAgent: true, wantLookup: true},
		{name: "missing catalog", noCatalog: true, wantLookup: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			agents := NewAgentService(t.TempDir())
			agent := Agent{Name: "preview-agent", AcpBackend: AcpBackendCursor, Capabilities: &models.AgentCapabilities{
				Interaction: models.InteractionAuto, Tools: []string{models.ToolSetPreview},
			}}
			sb := models.Sandbox{ID: 312, Name: "sb-solo", RunID: "solo-session-generation", Token: "persisted-token", ThreadID: "session", Profile: agent.Name, Purpose: SandboxPurposeSolo, Status: "running"}
			session := models.SoloSession{ID: "session", AgentName: agent.Name, SandboxID: sb.ID, Status: models.SoloStatusRunning}
			if tc.change != nil {
				tc.change(&sb, &session, &agent)
			}
			if !tc.noAgent {
				if err := agents.Save(agent); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Create(&sb).Error; err != nil {
				t.Fatal(err)
			}
			if !tc.noSession {
				if err := db.Create(&session).Error; err != nil {
					t.Fatal(err)
				}
			}
			svc := NewPreviewService(db, nil)
			if !tc.noCatalog {
				svc.SetAgents(agents)
			}
			nodeID := tc.nodeID
			if nodeID == "" {
				nodeID = "solo"
			}
			name, ok := svc.SandboxForRunNode(sb.RunID, nodeID)
			if ok != tc.wantLookup || (ok && name != sb.Name) {
				t.Fatalf("SandboxForRunNode = %q, %v; want lookup %v", name, ok, tc.wantLookup)
			}
			if got := svc.DirectPreview(sb.RunID, nodeID); got != tc.wantDirect {
				t.Fatalf("DirectPreview = %v; want %v", got, tc.wantDirect)
			}
			binding, caps, ok := svc.SoloMCPBinding(sb.RunID)
			if ok != tc.wantBinding || (ok && (binding.ID != sb.ID || binding.Token != sb.Token)) {
				t.Fatalf("SoloMCPBinding = %+v, %v; want binding %v", binding, ok, tc.wantBinding)
			}
			if ok && caps.CanPreview() != agent.Capabilities.CanPreview() {
				t.Fatalf("binding capabilities = %+v; want %+v", caps, agent.Capabilities)
			}
		})
	}
}

func TestPreviewServiceWorkflowSharedKeepsGraphPermissions(t *testing.T) {
	db := newTestDB(t)
	svc := NewPreviewService(db, nil)
	// A workflow keeps its snapshotted graph permissions, even when its shared
	// sandbox's current Agent configuration has a different preview grant.
	svc.SetAgents(fakeAgentLookup{"current-agent": {Name: "current-agent", Capabilities: testAutoCaps}})
	run := models.Run{ID: "shared-workflow", Graph: models.Graph{Nodes: []models.Node{
		agentNode("n1", "preview", testReviewCaps), agentNode("n2", "plain", testAutoCaps),
	}}}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	shared := models.Sandbox{Name: "sb-shared", RunID: run.ID, NodeID: "n2", Profile: "current-agent", Purpose: "run", Shared: true, Status: "running"}
	if err := db.Create(&shared).Error; err != nil {
		t.Fatal(err)
	}
	if name, ok := svc.SandboxForRunNode(run.ID, "n1"); !ok || name != shared.Name {
		t.Fatalf("shared workflow lookup = %q, %v", name, ok)
	}
	if !svc.DirectPreview(run.ID, "n1") || svc.DirectPreview(run.ID, "n2") {
		t.Fatal("shared workflow permissions must come from the requested graph node")
	}
	if _, _, ok := svc.SoloMCPBinding(run.ID); ok {
		t.Fatal("workflow sandbox resolved as a Solo MCP binding")
	}
}

func TestPreviewServiceSoloDatabaseFailureDoesNotGrantPreview(t *testing.T) {
	db := newTestDB(t)
	svc := NewPreviewService(db, nil)
	svc.SetAgents(fakeAgentLookup{"preview-agent": {Name: "preview-agent", Capabilities: testReviewCaps}})
	sb := models.Sandbox{Name: "sb-solo", RunID: "solo-current", ThreadID: "session", Profile: "preview-agent", Purpose: SandboxPurposeSolo}
	if err := db.Create(&sb).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{ID: "session", SandboxID: sb.ID, AgentName: sb.Profile, Status: models.SoloStatusRunning}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&models.Run{}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := svc.SoloMCPBinding(sb.RunID); !ok {
		t.Fatal("test fixture must remain a valid Solo binding")
	}
	if svc.DirectPreview(sb.RunID, "solo") {
		t.Fatal("a failed workflow query must not be treated as a missing workflow")
	}
	if _, _, ok := (&PreviewService{}).SoloMCPBinding(sb.RunID); ok {
		t.Fatal("unconfigured service returned a Solo binding")
	}
	if _, ok := (&PreviewService{}).SandboxForRunNode(sb.RunID, "solo"); ok {
		t.Fatal("unconfigured service resolved a sandbox")
	}
	if _, ok := svc.SandboxForRunNode("", "solo"); ok {
		t.Fatal("empty run ID resolved a sandbox")
	}
	if _, ok := svc.SandboxForRunNode(sb.RunID, ""); ok {
		t.Fatal("empty node ID resolved a sandbox")
	}
}

func TestPreviewServiceSoloSetPreview(t *testing.T) {
	db := newTestDB(t)
	agents := NewAgentService(t.TempDir())
	agent := Agent{Name: "preview-agent", AcpBackend: AcpBackendCursor, Capabilities: &models.AgentCapabilities{
		Interaction: models.InteractionAuto, Tools: []string{models.ToolSetPreview},
	}}
	if err := agents.Save(agent); err != nil {
		t.Fatal(err)
	}

	// The real platform probe must reach the published injector, whose header
	// proves it now serves app port 3080 rather than a previous preview.
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(injectUpstreamHeader, "3080")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(app.Close)
	fg := sandboxtest.New(t)
	fg.Seed("sb-solo")
	fg.SetEndpoints("sb-solo", map[string]string{"session": "127.0.0.1:34567", "ssh": "127.0.0.1:2222", "18081": app.Listener.Addr().String()})
	mgr := sandbox.NewManager(fg.Client(), sandbox.ManagerOptions{WorkspaceDir: "/root/workspace"})
	svc := NewPreviewService(db, mgr)
	svc.SetAgents(agents)
	host := mcp.NewHost(NewArtifactService(db))
	host.SetPreviewStore(svc)
	host.SetPreviewSandboxOps(svc)
	const runID = "solo-current-generation"
	token := host.RegisterRun(runID)
	host.SetActiveNode(runID, "solo", agent.Capabilities.Clone())
	sb := models.Sandbox{Name: "sb-solo", RunID: runID, Token: token, Profile: agent.Name, ThreadID: "session", Purpose: SandboxPurposeSolo, Status: "running"}
	if err := db.Create(&sb).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.SoloSession{ID: "session", SandboxID: sb.ID, AgentName: agent.Name, Status: models.SoloStatusRunning}).Error; err != nil {
		t.Fatal(err)
	}
	var switched, keptAlive bool
	t.Cleanup(sandbox.SetExecHook(func(_ context.Context, _ string, _ int, _ string, stdin io.Reader) ([]byte, error) {
		script, err := io.ReadAll(stdin)
		if err != nil {
			return nil, err
		}
		if strings.Contains(string(script), `exec "$s" set-upstream 3080`) {
			switched = true
			return []byte("result=ok preview_port=18081 upstream=3080\n"), nil
		}
		if strings.Contains(string(script), "port=3080") && strings.Contains(string(script), "setsid") {
			keptAlive = true
			return []byte("OK pid=4242 detached=1\n"), nil
		}
		t.Errorf("unexpected sandbox script: %s", script)
		return nil, nil
	}))

	call := func() (bool, string) {
		t.Helper()
		status, body := host.ServeRPC(runID, token, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"set_preview","arguments":{"port":3080,"label":"Solo app"}}}`))
		var response struct {
			Result struct {
				IsError bool                    `json:"isError"`
				Content []struct{ Text string } `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &response); err != nil || status != http.StatusOK || len(response.Result.Content) != 1 {
			t.Fatalf("RPC response: status=%d body=%s err=%v", status, body, err)
		}
		return response.Result.IsError, response.Result.Content[0].Text
	}
	if isError, text := call(); isError || !strings.Contains(text, "已注入取点脚本") {
		t.Fatalf("set_preview: isError=%v text=%s", isError, text)
	}
	if !switched || !keptAlive {
		t.Fatalf("preview did not switch injector and preserve listener: switched=%v keptAlive=%v", switched, keptAlive)
	}
	rec, ok := svc.GetPreviewPort(runID, "solo", 3080)
	if !ok || !rec.Healthy || rec.Host != app.URL || rec.SandboxName != sb.Name || rec.KeepalivePID != 4242 {
		t.Fatalf("persisted preview = %+v, %v", rec, ok)
	}

	host.SetActiveNode(runID, "other", agent.Capabilities.Clone())
	if isError, text := call(); !isError || !strings.Contains(text, "沙箱未就绪") {
		t.Fatalf("wrong node registered Solo preview: isError=%v text=%s", isError, text)
	}
	host.SetActiveNode(runID, "solo", &models.AgentCapabilities{Interaction: models.InteractionAuto})
	if isError, text := call(); !isError {
		t.Fatalf("ungranted Agent registered Solo preview: %s", text)
	}
}
