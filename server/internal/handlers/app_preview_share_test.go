package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestAppPreviewShareModesAndProxyScope(t *testing.T) {
	for _, solo := range []bool{true, false} {
		name := "workflow"
		if solo {
			name = "solo"
		}
		t.Run(name, func(t *testing.T) {
			hn := newHarness(t)
			hn.h.Preview = services.NewPreviewService(hn.db, nil)
			hn.h.Preview.SetAgents(hn.h.Agents)
			hn.host.SetPreviewStore(hn.h.Preview)
			hn.host.SetPreviewSandboxOps(hn.h.Preview)
			// Use a plain HTTP app upstream. Gateway health is covered by the
			// registered preview proxy integration tests.
			hn.h.Sbx = nil
			app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Error("app received Grasp credentials")
				}
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Set-Cookie", "example=app")
				_, _ = w.Write([]byte(`<script src="/app.js"></script><p>Shared app body</p>`))
			}))
			defer app.Close()
			runID, nodeID := "run-preview-share", "review"
			mainHistory := "请复审 research.json"
			sb := models.Sandbox{Name: "share-sandbox", RunID: runID, NodeID: nodeID, Profile: "agent", Purpose: "run", Status: "running"}
			manage := "/api/runs/" + runID + "/nodes/" + nodeID + "/preview-share"
			if solo {
				runID, nodeID = "solo-preview-share", "solo"
				sb.RunID, sb.NodeID, sb.Purpose, sb.ThreadID = runID, "", services.SandboxPurposeSolo, "share-solo"
				manage = "/api/solo/sessions/" + sb.ThreadID + "/preview-share"
				mainHistory = "Private Solo main history"
			} else {
				seedInboxReview(t, hn, runID, nodeID, false)
			}
			if err := hn.db.Create(&sb).Error; err != nil {
				t.Fatal(err)
			}
			hn.h.ModelConfigurations = services.NewModelConfigurationService(hn.db)
			hn.h.ModelConnections = services.NewModelConnectionService(hn.db)
			binding := modelrouting.Binding{
				Kind: "platform", CredentialID: "private-key-id",
				ModelID: "primary-model", AuxiliaryModelIDs: []string{"preview-model", "next-model"},
			}
			for _, id := range []string{"preview-model-group", "unrelated-model-group"} {
				if err := hn.db.Create(&models.ModelConfiguration{ID: id, State: "active", Backend: services.AcpBackendCursor, Binding: binding}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := hn.h.Agents.Save(services.Agent{
				Name: sb.Profile, AcpBackend: services.AcpBackendCursor, Capabilities: testPreviewCaps,
				ModelBinding: modelrouting.ConfigurationReference("preview-model-group"),
			}); err != nil {
				t.Fatal(err)
			}
			if solo {
				hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, nil)
				if err := hn.db.Create(&models.SoloSession{ID: sb.ThreadID, UserID: "admin", AgentName: sb.Profile, SandboxID: sb.ID, Status: models.SoloStatusRunning}).Error; err != nil {
					t.Fatal(err)
				}
				if err := hn.db.Create(&models.SoloMessage{ID: "solo-history", SessionID: sb.ThreadID, Role: "user", Content: mainHistory}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if !solo {
				var run models.Run
				if err := hn.db.First(&run, "id = ?", runID).Error; err != nil {
					t.Fatal(err)
				}
				run.Graph.FindNode(nodeID).Config = map[string]any{models.AgentProfileKey: sb.Profile}
				if err := hn.db.Save(&run).Error; err != nil {
					t.Fatal(err)
				}
				seedDirectPreview(t, hn, runID, nodeID)
				owner := redeem(t, hn, issueSessionTicket(t, hn, runID, nodeID))["token"].(string)
				modelPath := "/embed-api/runs/" + runID + "/nodes/"
				w := hn.doEmbed(http.MethodPost, modelPath+nodeID+"/model", map[string]any{
					"iteration": 1, "modelId": "preview-model",
				}, bearerHeader(owner))
				if w.Code != http.StatusOK || parseJSON(t, w)["currentModelId"] != "preview-model" {
					t.Fatalf("owner drawer model: %d %s", w.Code, w.Body)
				}
				if w := hn.doEmbed(http.MethodGet, modelPath+"other/model-catalog", nil, bearerHeader(owner)); w.Code != http.StatusUnauthorized {
					t.Fatalf("owner drawer token reached another node: %d", w.Code)
				}
			}
			if err := hn.h.Preview.UpsertPreviewPort(mcp.PreviewPort{RunID: runID, NodeID: nodeID, Port: 8080, Host: app.URL, Healthy: true}); err != nil {
				t.Fatal(err)
			}
			if w := hn.do(http.MethodPost, manage, map[string]any{"port": 9999, "chatMode": "isolated"}); w.Code != http.StatusNotFound {
				t.Fatalf("unregistered port: %d", w.Code)
			}
			tokens := map[string]string{}
			for _, mode := range []string{"isolated", "main"} {
				w := hn.do(http.MethodPost, manage, map[string]any{"port": 8080, "chatMode": mode})
				if w.Code != http.StatusOK {
					t.Fatalf("create %s: %d %s", mode, w.Code, w.Body)
				}
				raw, _ := parseJSON(t, w)["url"].(string)
				_, tokens[mode], _ = strings.Cut(raw, "/public/approvals/app#t=")
			}
			headers := func(mode string) map[string]string {
				return map[string]string{headerShareToken: tokens[mode], headerShareRequest: "1", "Origin": "http://" + publicHost}
			}
			const path = "/public/approvals/app"
			for _, mode := range []string{"isolated", "main"} {
				w := hn.doEmbed(http.MethodPost, path+"/open", map[string]any{}, headers(mode))
				opened := parseJSON(t, w)
				ticket, _ := opened["ticket"].(string)
				if w.Code != http.StatusOK || opened["directUrl"] != app.URL+"/" || ticket == "" || strings.Contains(w.Body.String(), tokens[mode]) {
					t.Fatalf("open %s: %d %s", mode, w.Code, w.Body)
				}
				res := redeem(t, hn, ticket)
				token, _ := res["token"].(string)
				if token == "" || res["kind"] != models.EmbedKindAppPreview {
					t.Fatalf("share drawer claims: %v", res)
				}
				header := headers(mode)
				header[headerShareToken] = token
				session := hn.doEmbed(http.MethodPost, path+"/session", map[string]any{}, header)
				if session.Code != http.StatusOK || parseJSON(t, session)["chatMode"] != mode {
					t.Fatalf("drawer session: %d %s", session.Code, session.Body)
				}
				want := http.StatusForbidden
				if mode == "main" {
					want = http.StatusOK
				}
				if w := hn.doEmbed(http.MethodGet, path+"/main-chat", nil, header); w.Code != want {
					t.Fatalf("drawer history %s: %d", mode, w.Code)
				}
				if w := hn.doEmbed(http.MethodPost, path+"/open", map[string]any{}, header); w.Code != http.StatusGone {
					t.Fatalf("drawer token minted a new ticket: %d", w.Code)
				}
				if w := hn.doEmbed(http.MethodGet, "/embed-api/solo/"+runID+"/messages", nil, bearerHeader(token)); w.Code != http.StatusUnauthorized {
					t.Fatalf("share token reached owner history: %d", w.Code)
				}
				tokens[mode+"-drawer"] = token
			}
			if w := hn.doEmbed(http.MethodGet, path+"/main-chat", nil, headers("isolated")); w.Code != http.StatusForbidden {
				t.Fatalf("isolated link can read main chat: %d", w.Code)
			}
			if w := hn.doEmbed(http.MethodGet, path+"/main-chat", nil, headers("main")); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), mainHistory) {
				t.Fatalf("main history: %d %s", w.Code, w.Body)
			}
			mainHeaders := headers("main-drawer")
			catalog := hn.doEmbed(http.MethodGet, path+"/model-catalog", nil, mainHeaders)
			if catalog.Code != http.StatusOK || !strings.Contains(catalog.Body.String(), "preview-model") ||
				strings.Contains(catalog.Body.String(), "private-key-id") || strings.Contains(catalog.Body.String(), "unrelated-model-group") {
				t.Fatalf("scoped catalog: %d %s", catalog.Code, catalog.Body)
			}
			body := map[string]any{"iteration": 99, "modelId": "next-model"}
			if w := hn.doEmbed(http.MethodPost, path+"/model", body, headers("isolated-drawer")); w.Code != http.StatusForbidden {
				t.Fatalf("independent link can switch the main chat model: %d", w.Code)
			}
			w := hn.doEmbed(http.MethodPost, path+"/model", body, mainHeaders)
			if w.Code != http.StatusOK || parseJSON(t, w)["currentModelId"] != "next-model" {
				t.Fatalf("shared main model: %d %s", w.Code, w.Body)
			}
			w = hn.doEmbed(http.MethodGet, path+"/model", nil, mainHeaders)
			if w.Code != http.StatusOK || parseJSON(t, w)["currentModelId"] != "next-model" {
				t.Fatalf("shared main persisted model: %d %s", w.Code, w.Body)
			}
			if w := hn.doEmbed(http.MethodPost, path+"/model", map[string]any{"modelId": "unlisted-model"}, mainHeaders); w.Code != http.StatusBadRequest {
				t.Fatalf("model outside bound group accepted: %d %s", w.Code, w.Body)
			}
			session := hn.doEmbed(http.MethodPost, path+"/session", map[string]any{}, headers("isolated"))
			if session.Code != http.StatusOK {
				t.Fatalf("exchange: %d %s", session.Code, session.Body)
			}
			reply := parseJSON(t, session)
			framePath, _ := reply["iframePath"].(string)
			renew := hn.doEmbed(http.MethodPost, path+"/session", map[string]any{"ticket": reply["ticket"]}, headers("isolated"))
			if renew.Code != http.StatusOK || parseJSON(t, renew)["iframePath"] != framePath {
				t.Fatalf("renew changed app address: %d %s", renew.Code, renew.Body)
			}
			proxy := &closeNotifyRecorder{ResponseRecorder: httptest.NewRecorder(), notify: make(chan bool)}
			request := httptest.NewRequest(http.MethodGet, framePath, nil)
			request.Header.Set("Cookie", "grasp=private")
			request.Header.Set("Authorization", "Bearer private")
			request.Header.Set("Origin", "null")
			hn.r.ServeHTTP(proxy, request)
			if proxy.Code != http.StatusOK || !strings.Contains(proxy.Body.String(), framePath+"app.js") ||
				!strings.Contains(strings.Join(proxy.Header().Values("Content-Security-Policy"), " "), "sandbox allow-scripts allow-forms") ||
				proxy.Header().Get("Set-Cookie") != "" || proxy.Header().Get("Access-Control-Allow-Origin") != "null" {
				t.Fatalf("app proxy: %d %v %s", proxy.Code, proxy.Header(), proxy.Body)
			}
			if w := hn.do(http.MethodDelete, manage, map[string]any{"port": 8080, "chatMode": "isolated"}); w.Code != http.StatusOK {
				t.Fatalf("revoke: %d %s", w.Code, w.Body)
			}
			if w := hn.doEmbed(http.MethodPost, path+"/session", map[string]any{}, headers("isolated")); w.Code != http.StatusGone {
				t.Fatalf("revoked exchange: %d", w.Code)
			}
			if w := hn.doEmbed(http.MethodPost, path+"/session", map[string]any{}, headers("isolated-drawer")); w.Code != http.StatusGone {
				t.Fatalf("revoked drawer: %d", w.Code)
			}
			if w := hn.doEmbed(http.MethodPost, path+"/session", map[string]any{}, headers("main")); w.Code != http.StatusOK {
				t.Fatalf("revoking isolated affected main: %d", w.Code)
			}
		})
	}
}
