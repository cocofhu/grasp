package handlers_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/blob"
	"github.com/cocofhu/grasp/internal/engine"
	"github.com/cocofhu/grasp/internal/handlers"
	"github.com/cocofhu/grasp/internal/mcp"
	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/runtime"
)

func TestLiveSessionsHTTP(t *testing.T) {
	h := newHarness(t)
	seedInboxReview(t, h, "run-live", "research1", true)
	w := h.do(http.MethodGet, "/api/runs/run-live/nodes/research1/live-sessions", nil)
	out := parseJSON(t, w)
	if w.Code != 200 || out["enabled"] != false {
		t.Fatalf("live sessions: %d %s", w.Code, w.Body.String())
	}
	if s, ok := out["sessions"].([]any); !ok || len(s) != 0 {
		t.Fatalf("sessions = %v", out["sessions"])
	}
	d := h.do(http.MethodPost, "/api/runs/run-live/nodes/research1/live-sessions/discard-all", nil)
	if d.Code != 200 || parseJSON(t, d)["discarding"] != float64(0) {
		t.Fatalf("discard all: %d %s", d.Code, d.Body.String())
	}
	r := h.do(http.MethodPost, "/api/runs/run-live/react/research1/reply", map[string]any{
		"live": map[string]any{"op": "discard", "sid": "sid001"},
	})
	if r.Code != http.StatusBadRequest || !strings.Contains(r.Body.String(), "Live") {
		t.Fatalf("live reply on non-live node: %d %s", r.Code, r.Body.String())
	}
}

func TestPublicLiveSessionsHTTP(t *testing.T) {
	h := newHarness(t)
	seedInboxReview(t, h, "run-live-pub", "research1", true)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-live-pub/reviews/research1/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	bad := h.doPublic(http.MethodGet, "/public/gate-approvals/live-sessions", nil, map[string]string{headerShareToken: "nope"})
	if parseJSON(t, bad)["status"] != "invalid" {
		t.Fatalf("bad token: %s", bad.Body.String())
	}
	ok := h.doPublic(http.MethodGet, "/public/gate-approvals/live-sessions", nil, map[string]string{headerShareToken: token})
	out := parseJSON(t, ok)
	if out["status"] != "active" || out["enabled"] != false {
		t.Fatalf("live sessions: %s", ok.Body.String())
	}
	hdr := map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost}
	d := h.doPublic(http.MethodPost, "/public/gate-approvals/live-discard-all", map[string]any{"token": token}, hdr)
	if d.Code != 200 || parseJSON(t, d)["discarding"] != float64(0) {
		t.Fatalf("discard all: %d %s", d.Code, d.Body.String())
	}
	csrf := h.doPublic(http.MethodPost, "/public/gate-approvals/live-discard-all", map[string]any{"token": token}, nil)
	if csrf.Code != http.StatusForbidden {
		t.Fatalf("csrf: %d", csrf.Code)
	}
	reply := h.doPublic(http.MethodPost, "/public/gate-approvals/reply", map[string]any{
		"token": token, "live": map[string]any{"op": "discard", "sid": "sid001"},
	}, hdr)
	if reply.Code != http.StatusBadRequest || !strings.Contains(reply.Body.String(), "Live") {
		t.Fatalf("live reply: %d %s", reply.Code, reply.Body.String())
	}
	ctxReply := h.doPublic(http.MethodPost, "/public/gate-approvals/reply", map[string]any{
		"token": token, "text": "标题再大点", "liveCtx": map[string]any{"sid": "sid001", "current": 1},
	}, hdr)
	if ctxReply.Code == http.StatusForbidden {
		t.Fatalf("ctx reply: %d %s", ctxReply.Code, ctxReply.Body.String())
	}
}

func TestPublicLiveReactOnlyDeniesLiveWrites(t *testing.T) {
	h := newHarness(t)
	seedInboxReview(t, h, "run-live-ro", "research1", true)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-live-ro/reviews/research1/share-link", map[string]any{
		"ttlTier": "24h", "permissionPreset": "react_only",
	}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")
	hdr := map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost}

	d := h.doPublic(http.MethodPost, "/public/gate-approvals/live-discard-all", map[string]any{"token": token}, hdr)
	if d.Code != http.StatusForbidden || !strings.Contains(d.Body.String(), "permission_denied") {
		t.Fatalf("discard all: %d %s", d.Code, d.Body.String())
	}
	reply := h.doPublic(http.MethodPost, "/public/gate-approvals/reply", map[string]any{
		"token": token, "live": map[string]any{"op": "discard", "sid": "sid001"},
	}, hdr)
	if reply.Code != http.StatusForbidden || !strings.Contains(reply.Body.String(), "permission_denied") {
		t.Fatalf("live reply: %d %s", reply.Code, reply.Body.String())
	}
	pageReply := h.doPublic(http.MethodPost, "/public/gate-approvals/reply", map[string]any{
		"token": token, "live": models.LiveEvent{Op: models.LiveOpGenerate, SID: "page01", Scope: "page", Prompt: "给登录弹窗三个方案"},
	}, hdr)
	if pageReply.Code != http.StatusForbidden || !strings.Contains(pageReply.Body.String(), "permission_denied") {
		t.Fatalf("page generation bypassed react_only: %d %s", pageReply.Code, pageReply.Body.String())
	}
	ctxReply := h.doPublic(http.MethodPost, "/public/gate-approvals/reply", map[string]any{
		"token": token, "text": "标题再大点", "liveCtx": map[string]any{"sid": "sid001", "current": 1},
	}, hdr)
	if ctxReply.Code == http.StatusForbidden {
		t.Fatalf("ctx reply should stay allowed: %d %s", ctxReply.Code, ctxReply.Body.String())
	}
}

func TestPublicLiveSessionStripsRunID(t *testing.T) {
	m := handlers.HandlersPublicLiveSessionForTest()
	if handlers.HandlersPublicLiveSessionForTest()["nodeId"] != nil {
		t.Fatal("nodeId leaked")
	}
	if _, ok := m["runId"]; ok {
		t.Fatalf("runId leaked: %v", m)
	}
	if m["sid"] != "sid001" || m["state"] != "ready" {
		t.Fatalf("fields: %v", m)
	}
}

type livePermissionProvider struct {
	fakeProvider
	eng     *engine.Engine
	reports chan error
}

func (p *livePermissionProvider) ReviseInPlace(_ context.Context, req runtime.NodeReq, _ []models.ReactMessage, _ string, _ []models.PromptImage) runtime.ReactTurn {
	_, err := p.eng.ApplyLiveReport(req.RunID, req.NodeID, mcp.LiveReport{SID: "chat01", State: models.LiveStateAccepting})
	p.reports <- err
	return runtime.ReactTurn{Msg: "已回复"}
}

func (p *livePermissionProvider) ReactReply(ctx context.Context, req runtime.NodeReq, history []models.ReactMessage, human string, images []models.PromptImage, _ bool) runtime.ReactTurn {
	return p.ReviseInPlace(ctx, req, history, human, images)
}

func (*livePermissionProvider) OfferCommitOnConfirm(context.Context, runtime.NodeReq) runtime.ReactTurn {
	return runtime.ReactTurn{}
}
func (*livePermissionProvider) ReconcileOnConfirm(context.Context, runtime.NodeReq) runtime.ReactTurn {
	return runtime.ReactTurn{}
}
func (*livePermissionProvider) HasLiveSession(string, string) bool { return true }
func (*livePermissionProvider) RetireSession(string, string)       {}

func TestPublicLiveChatBeginUsesSharePermission(t *testing.T) {
	for name, caps := range map[string]*models.AgentCapabilities{"review": testPreviewCaps, "clarify": testClarifyCaps} {
		for _, permission := range []string{models.SharePermissionFull, models.SharePermissionReactOnly} {
			t.Run(name+"/"+permission, func(t *testing.T) {
				h := newHarness(t)
				seedInboxReview(t, h, "run-live-chat", "preview", true)
				var run models.Run
				h.db.First(&run, "id = ?", "run-live-chat")
				run.Graph.Nodes[0].Type = "agent"
				run.Graph.Nodes[0].Caps = caps
				h.db.Save(&run)
				h.db.Create(&models.LiveSession{ID: "chat01", RunID: run.ID, NodeID: "preview", Mode: "replace", State: models.LiveStateReady,
					Variants: []models.LiveVariant{{N: 1}}})
				provider := &livePermissionProvider{reports: make(chan error, 1)}
				eng := engine.New(h.db, provider, h.host, h.h.Arts, 5)
				provider.eng = eng
				h.h.Eng = eng
				t.Cleanup(eng.Close)
				created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-live-chat/reviews/preview/share-link", map[string]any{
					"ttlTier": "24h", "permissionPreset": permission,
				}))
				url, _ := created["url"].(string)
				if !strings.Contains(url, "#t=") {
					t.Fatalf("share link: %v", created)
				}
				token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")
				reply := h.doPublic(http.MethodPost, "/public/gate-approvals/reply", map[string]any{
					"token": token, "text": "就用这个", "liveCtx": map[string]any{"sid": "chat01", "current": 1, "params": map[string]any{"gap": "24px"}},
				}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
				if reply.Code != http.StatusOK {
					t.Fatalf("comment should remain allowed: %d %s", reply.Code, reply.Body.String())
				}
				select {
				case err := <-provider.reports:
					if permission == models.SharePermissionReactOnly {
						if err == nil || !strings.Contains(err.Error(), "权限") {
							t.Fatalf("react_only authorized implicit adoption: %v", err)
						}
					} else if err != nil {
						t.Fatalf("full share could not begin Chat adoption: %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("review turn did not run")
				}
			})
		}
	}
}

func TestLiveCapabilityForClarifyEmbedAndAuthenticatedPreview(t *testing.T) {
	for name, caps := range map[string]*models.AgentCapabilities{"review": testPreviewCaps, "clarify": testClarifyCaps} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			runID := "run-live-embed"
			nodeID := "clarify-preview"
			seedInboxReview(t, h, runID, nodeID, true)
			var run models.Run
			h.db.First(&run, "id = ?", runID)
			run.Graph.FindNode(nodeID).Caps = caps
			h.db.Save(&run)
			seedDirectPreview(t, h, runID, nodeID)
			authenticated := h.do(http.MethodGet, "/api/runs/"+runID+"/nodes/"+nodeID+"/live-sessions", nil)
			if authenticated.Code != http.StatusOK || parseJSON(t, authenticated)["enabled"] != true {
				t.Fatalf("authenticated Live: %d %s", authenticated.Code, authenticated.Body.String())
			}
			token, _ := redeem(t, h, issueSessionTicket(t, h, runID, nodeID))["token"].(string)
			if token == "" {
				t.Fatal("missing drawer token")
			}
			response := h.doPublic(http.MethodGet, "/public/gate-approvals/live-sessions", nil, map[string]string{headerShareToken: token})
			body := parseJSON(t, response)
			if response.Code != http.StatusOK || body["enabled"] != true || body["status"] != "active" {
				t.Fatalf("drawer Live: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

type chatLiveCall struct {
	human  string
	images []models.PromptImage
	err    error
}

type chatLiveProvider struct {
	livePermissionProvider
	calls chan chatLiveCall
}

func (p *chatLiveProvider) ReviseInPlace(_ context.Context, req runtime.NodeReq, _ []models.ReactMessage, human string, images []models.PromptImage) runtime.ReactTurn {
	_, err := p.eng.ApplyLiveReport(req.RunID, req.NodeID, mcp.LiveReport{SID: "chat01", State: models.LiveStateReady,
		File: "src/Dialog.vue", Variants: []models.LiveVariant{{N: 1, Label: "层级"}, {N: 2, Label: "紧凑"}, {N: 3, Label: "强调"}}})
	p.calls <- chatLiveCall{human: human, images: images, err: err}
	return runtime.ReactTurn{Msg: "请在页面上比较三个候选"}
}

func (p *chatLiveProvider) ReactReply(ctx context.Context, req runtime.NodeReq, history []models.ReactMessage, human string, images []models.PromptImage, _ bool) runtime.ReactTurn {
	return p.ReviseInPlace(ctx, req, history, human, images)
}

func TestChatLiveGenerateThroughAuthenticatedAndEmbedReplies(t *testing.T) {
	cases := []struct {
		name string
		caps *models.AgentCapabilities
	}{{"review", testPreviewCaps}, {"clarify-attachments", testClarifyCaps}, {"clarify-text", testClarifyCaps}}
	for _, tc := range cases {
		for _, entry := range []string{"authenticated", "embed"} {
			t.Run(tc.name+"/"+entry, func(t *testing.T) {
				h := newHarness(t)
				runID, nodeID := "run-chat-live", "preview"
				seedInboxReview(t, h, runID, nodeID, true)
				var run models.Run
				h.db.First(&run, "id = ?", runID)
				run.Graph.FindNode(nodeID).Caps = tc.caps
				h.db.Save(&run)
				seedDirectPreview(t, h, runID, nodeID)
				provider := &chatLiveProvider{calls: make(chan chatLiveCall, 1)}
				eng := engine.New(h.db, provider, h.host, h.h.Arts, 5)
				eng.SetBlobStore(blob.NewMemory())
				provider.eng = eng
				h.h.Eng = eng
				t.Cleanup(eng.Close)
				prompt := strings.Repeat("把登录弹窗改得更易读。", 12)
				images := []models.PromptImage{{Data: "QUJD", MimeType: "image/png", Name: "reference.png"}}
				annotations := []models.ReactAnnotation{{Selector: "#login-dialog", Text: "按钮需要更明显"}}
				// Preserve ordinary composer support for attachment-only turns.
				if tc.name == "clarify-attachments" {
					prompt, annotations = "", nil
				}
				if tc.name == "clarify-text" {
					prompt, images = "", nil
				}
				body := map[string]any{"live": models.LiveEvent{Op: models.LiveOpGenerate, SID: "chat01", Scope: "page", Prompt: prompt, Count: 3}, "images": images, "annotations": annotations}
				var code int
				var response string
				if entry == "embed" {
					token, _ := redeem(t, h, issueSessionTicket(t, h, runID, nodeID))["token"].(string)
					body["token"] = token
					w := h.doPublic(http.MethodPost, "/public/gate-approvals/reply", body, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
					code, response = w.Code, w.Body.String()
				} else {
					w := h.do(http.MethodPost, "/api/runs/"+runID+"/react/"+nodeID+"/reply", body)
					code, response = w.Code, w.Body.String()
				}
				if code != http.StatusOK || !strings.Contains(response, `"live"`) {
					t.Fatalf("chat generate: %d %s", code, response)
				}
				select {
				case call := <-provider.calls:
					if call.err != nil || !strings.Contains(call.human, "scope: page") || !strings.Contains(call.human, "等待用户选择") || !strings.Contains(call.human, prompt) {
						t.Fatalf("agent did not receive page choices contract: %+v", call)
					}
					if len(call.images) != len(images) || (len(images) > 0 && (call.images[0].Data != "" || !strings.HasPrefix(call.images[0].Ref, "blob:"))) {
						t.Fatalf("attachments lost or not externalized: %+v", call.images)
					}
					if len(annotations) > 0 && !strings.Contains(call.human, "按钮需要更明显") {
						t.Fatalf("annotations omitted from agent prompt: %s", call.human)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("Live chat turn did not reach agent")
				}
				var conv models.ReactConversation
				h.db.Where("run_id = ? AND node_id = ?", runID, nodeID).First(&conv)
				var human *models.ReactMessage
				for i := range conv.Messages {
					if conv.Messages[i].Role == "human" {
						human = &conv.Messages[i]
					}
				}
				if human == nil || human.Live == nil || human.Live.SID != "chat01" || human.Text == "" || (prompt != "" && human.Text != prompt) || len(human.Images) != len(images) || len(human.Annotations) != len(annotations) {
					t.Fatalf("Live human turn lost context: %+v", human)
				}
				if conv.Done {
					t.Fatal("candidate generation completed the dialogue before user selection")
				}
			})
		}
	}
}
