package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
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
			if solo {
				if err := hn.h.Agents.Save(services.Agent{Name: sb.Profile, AcpBackend: services.AcpBackendCursor, Capabilities: testPreviewCaps}); err != nil {
					t.Fatal(err)
				}
				hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, nil)
				if err := hn.db.Create(&models.SoloSession{ID: sb.ThreadID, UserID: "admin", AgentName: sb.Profile, SandboxID: sb.ID, Status: models.SoloStatusRunning}).Error; err != nil {
					t.Fatal(err)
				}
				if err := hn.db.Create(&models.SoloMessage{ID: "solo-history", SessionID: sb.ThreadID, Role: "user", Content: mainHistory}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if !solo {
				seedDirectPreview(t, hn, runID, nodeID)
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
