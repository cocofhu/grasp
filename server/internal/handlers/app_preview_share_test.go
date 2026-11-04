package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/modelrouting"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
	"github.com/gorilla/websocket"
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
			guestFile := models.PromptImage{MimeType: "text/plain", Name: "guest-notes.txt", Data: "Z3Vlc3Qgbm90ZXM="}
			ownerFile := models.PromptImage{MimeType: "text/plain", Name: "owner-private.txt", Data: "cHJpdmF0ZQ=="}
			picks := []models.ReactAnnotation{{Selector: "#heading", URL: "https://preview.example.test/hidden", OuterHTML: "<h1>private-markup</h1>"}}
			parts := []models.AcpPart{{Kind: "thought", Text: "Inspect the heading"},
				{Kind: "tool", Title: "read_file /private/notes", Status: "completed", Input: "private-tool-input", Output: "private-tool-output"},
				{Kind: "message", Text: "Preview timeline reply"}}
			if solo {
				hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, nil)
				if err := hn.db.Create(&models.SoloSession{ID: sb.ThreadID, UserID: "admin", AgentName: sb.Profile, SandboxID: sb.ID, Status: models.SoloStatusRunning}).Error; err != nil {
					t.Fatal(err)
				}
				if err := hn.db.Create(&models.SoloMessage{ID: "solo-history", SessionID: sb.ThreadID, Role: "user", Content: mainHistory, Annotations: picks, Images: []models.PromptImage{ownerFile}}).Error; err != nil {
					t.Fatal(err)
				}
				if err := hn.db.Create(&models.SoloMessage{
					ID: "timeline-reply", SessionID: sb.ThreadID, Role: "assistant", Content: "Preview timeline reply",
					DurationMs: 2000, Events: []json.RawMessage{
						json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_thought_chunk","content":{"text":"Inspect the heading"}}}`),
						json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"tool_call","toolCallId":"private-id","title":"read_file /private/notes","status":"completed","rawInput":{"path":"private-tool-input"},"rawOutput":"private-tool-output"}}`),
						json.RawMessage(`{"type":"session_update","update":{"sessionUpdate":"agent_message_chunk","content":{"text":"Preview timeline reply"}}}`),
					},
				}).Error; err != nil {
					t.Fatal(err)
				}
				if err := hn.db.Create(&models.SoloMessage{
					ID: "visitor-upload", SessionID: sb.ThreadID, Role: "user", Content: "Visitor upload",
					PreviewShared: true, Images: []models.PromptImage{guestFile},
				}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if !solo {
				var conv models.ReactConversation
				if err := hn.db.First(&conv, "run_id = ? AND node_id = ?", runID, nodeID).Error; err != nil {
					t.Fatal(err)
				}
				conv.Messages = append(conv.Messages,
					models.ReactMessage{Role: "agent", Text: "Preview timeline reply", Parts: parts, At: time.Now().Format(time.RFC3339Nano), DurationMs: 2000},
					models.ReactMessage{Role: "human", Text: "Owner attachment", Annotations: picks, Images: []models.PromptImage{ownerFile}},
					models.ReactMessage{Role: "human", Text: "Visitor upload", PreviewShared: true, Images: []models.PromptImage{guestFile}},
				)
				if err := hn.db.Save(&conv).Error; err != nil {
					t.Fatal(err)
				}
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
				tokens[mode+"-api"] = parseJSON(t, session)["ticket"].(string)
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
				if w := hn.doEmbed(http.MethodGet, "/api/workspace", nil, bearerHeader(token)); w.Code != http.StatusUnauthorized {
					t.Fatalf("share token reached the workspace API: %d", w.Code)
				}
				tokens[mode+"-drawer"] = token
			}
			if w := hn.doEmbed(http.MethodGet, path+"/main-chat", nil, headers("isolated")); w.Code != http.StatusForbidden {
				t.Fatalf("isolated link can read main chat: %d", w.Code)
			}
			if w := hn.doEmbed(http.MethodGet, path+"/main-chat", nil, headers("main")); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), mainHistory) ||
				!strings.Contains(w.Body.String(), `"selector":"#heading"`) ||
				strings.Contains(w.Body.String(), "preview.example.test") || strings.Contains(w.Body.String(), "private-markup") {
				t.Fatalf("main history: %d %s", w.Code, w.Body)
			}
			mainHeaders := headers("main-drawer")
			history := hn.doEmbed(http.MethodGet, path+"/main-chat", nil, mainHeaders)
			if !strings.Contains(history.Body.String(), `"kind":"thought","text":"Inspect the heading"`) ||
				!strings.Contains(history.Body.String(), `"kind":"tool","title":"read_file","status":"completed"`) ||
				!strings.Contains(history.Body.String(), `"startedAt"`) || !strings.Contains(history.Body.String(), `"endedAt"`) ||
				strings.Contains(history.Body.String(), "private-tool-") || strings.Contains(history.Body.String(), "/private/notes") {
				t.Fatalf("shared timeline: %d %s", history.Code, history.Body)
			}
			// Both main backends seed the public stream with the same timeline;
			// independent tickets and cross-origin sockets cannot subscribe.
			streamServer := httptest.NewServer(hn.r)
			defer streamServer.Close()
			dial := func(ticket, origin string) (*websocket.Conn, *http.Response, error) {
				d := websocket.Dialer{Subprotocols: []string{"grasp-preview-main", ticket}}
				return d.Dial(wsURL(streamServer.URL, path+"/main-events"), http.Header{"Origin": []string{origin}})
			}
			for _, tc := range []struct{ ticket, origin string }{
				{tokens["isolated-api"], streamServer.URL}, {tokens["main-api"], "https://other.example.test"},
			} {
				conn, response, err := dial(tc.ticket, tc.origin)
				if conn != nil {
					_ = conn.Close()
				}
				if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
					t.Fatalf("unauthorized stream: %v %+v", err, response)
				}
			}
			stream, _, err := dial(tokens["main-api"], streamServer.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			_ = stream.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, seed, err := stream.ReadMessage()
			if err != nil || !strings.Contains(string(seed), `"type":"snapshot"`) || !strings.Contains(string(seed), "Inspect the heading") || strings.Contains(string(seed), "private-tool-") {
				t.Fatalf("main stream seed: %s %v", seed, err)
			}
			if !strings.Contains(history.Body.String(), "guest-notes.txt") || strings.Contains(history.Body.String(), "owner-private.txt") ||
				strings.Contains(history.Body.String(), guestFile.Data) {
				t.Fatalf("attachment metadata: %d %s", history.Code, history.Body)
			}
			var guestID, ownerID string
			for _, raw := range parseJSON(t, history)["rows"].([]any) {
				row := raw.(map[string]any)
				switch row["text"] {
				case "Visitor upload":
					guestID = row["id"].(string)
				case mainHistory, "Owner attachment":
					ownerID = row["id"].(string)
				}
			}
			if guestID == "" || ownerID == "" {
				t.Fatal("missing history message IDs")
			}
			if w := hn.doEmbed(http.MethodGet, path+"/images/"+guestID+"/0", nil, mainHeaders); w.Code != http.StatusOK || w.Body.String() != "guest notes" {
				t.Fatalf("visitor attachment: %d %s", w.Code, w.Body)
			}
			if w := hn.doEmbed(http.MethodGet, path+"/images/"+ownerID+"/0", nil, mainHeaders); w.Code != http.StatusNotFound {
				t.Fatalf("owner attachment exposed: %d", w.Code)
			}
			if w := hn.doEmbed(http.MethodGet, path+"/images/"+guestID+"/0", nil, headers("isolated-drawer")); w.Code != http.StatusNotFound {
				t.Fatalf("independent link accessed shared attachment: %d", w.Code)
			}
			if w := hn.doEmbed(http.MethodGet, path+"/references/search?lane=solo", nil, mainHeaders); w.Code != http.StatusBadRequest {
				t.Fatalf("private Solo lookup accepted: %d", w.Code)
			}
			if w := hn.doEmbed(http.MethodPost, path+"/main-chat", map[string]any{"images": []models.PromptImage{{Ref: "blob:someone-elses-file"}}}, mainHeaders); w.Code != http.StatusBadRequest {
				t.Fatalf("arbitrary attachment reference accepted: %d", w.Code)
			}
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
			if !solo {
				hn.h.Eng.Broker().Publish(runID, []byte(`{"type":"dialogue","nodeId":"`+nodeID+`"}`))
			}
			for {
				_, pushed, err := stream.ReadMessage()
				if err != nil {
					t.Fatalf("main stream did not update: %v", err)
				}
				if strings.Contains(string(pushed), `"currentModelId":"next-model"`) {
					break
				}
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
