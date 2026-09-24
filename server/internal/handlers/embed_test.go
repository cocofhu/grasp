package handlers_test

import (
	"bytes"
	"encoding/json"
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

const embedDirectURL = "http://127.0.0.1:18080/"

func seedDirectPreview(t *testing.T, hn *harness, runID, nodeID string) {
	t.Helper()
	preview := services.NewPreviewService(hn.db, nil)
	hn.h.Preview = preview
	hn.host.SetPreviewStore(preview)
	hn.host.SetPreviewSandboxOps(preview)
	var run models.Run
	if err := hn.db.Where("id = ?", runID).First(&run).Error; err != nil {
		t.Fatal(err)
	}
	if n := run.Graph.FindNode(nodeID); n != nil {
		n.Config = map[string]any{"direct_preview": true}
	}
	if err := hn.db.Save(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := preview.UpsertPreviewPort(mcp.PreviewPort{
		RunID: runID, NodeID: nodeID, Port: 18080, Label: "Direct",
		Host: strings.TrimRight(embedDirectURL, "/"), Healthy: true, RegisteredAt: time.Now(),
		Mode: "direct", DirectURL: embedDirectURL,
	}); err != nil {
		t.Fatal(err)
	}
}

func (hn *harness) doEmbed(method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Host = publicHost
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	hn.r.ServeHTTP(w, req)
	return w
}

func redeemHeaders() map[string]string {
	return map[string]string{"X-Grasp-Embed": "1", "Origin": "http://" + publicHost}
}

func issueSessionTicket(t *testing.T, hn *harness, runID, nodeID string) string {
	t.Helper()
	w := hn.do(http.MethodPost, "/api/runs/"+runID+"/nodes/"+nodeID+"/embed-ticket", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("issue: %d %s", w.Code, w.Body.String())
	}
	ticket, _ := parseJSON(t, w)["ticket"].(string)
	if ticket == "" {
		t.Fatalf("no ticket: %s", w.Body.String())
	}
	return ticket
}

func redeem(t *testing.T, hn *harness, ticket string) map[string]any {
	t.Helper()
	w := hn.doEmbed(http.MethodPost, "/embed-api/session", map[string]any{"ticket": ticket}, redeemHeaders())
	if w.Code != http.StatusOK {
		t.Fatalf("redeem: %d %s", w.Code, w.Body.String())
	}
	return parseJSON(t, w)
}

func bearerHeader(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func TestEmbedTicketRequiresDirectPreview(t *testing.T) {
	hn := newHarness(t)
	seedAppPreviewReview(t, hn, "run-emb-nodirect", "ap1")
	w := hn.do(http.MethodPost, "/api/runs/run-emb-nodirect/nodes/ap1/embed-ticket", nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409 without direct preview, got %d %s", w.Code, w.Body.String())
	}
	if w := hn.doWithCookie(http.MethodPost, "/api/runs/run-emb-nodirect/nodes/ap1/embed-ticket", nil, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without session, got %d", w.Code)
	}
}

func TestEmbedSessionFlow(t *testing.T) {
	hn := newHarness(t)
	seedAppPreviewReview(t, hn, "run-emb", "ap1")
	seedAppPreviewReview(t, hn, "run-emb-other", "ap1")
	seedDirectPreview(t, hn, "run-emb", "ap1")

	ticket := issueSessionTicket(t, hn, "run-emb", "ap1")

	// Redemption must come from this origin with the custom header.
	if w := hn.doEmbed(http.MethodPost, "/embed-api/session", map[string]any{"ticket": ticket},
		map[string]string{"Origin": "http://" + publicHost}); w.Code != http.StatusForbidden {
		t.Fatalf("missing header: %d", w.Code)
	}
	if w := hn.doEmbed(http.MethodPost, "/embed-api/session", map[string]any{"ticket": ticket},
		map[string]string{"X-Grasp-Embed": "1", "Origin": "http://127.0.0.1:18080"}); w.Code != http.StatusForbidden {
		t.Fatalf("preview origin must not redeem: %d", w.Code)
	}
	if w := hn.doEmbed(http.MethodPost, "/embed-api/session", map[string]any{"ticket": ticket},
		map[string]string{"X-Grasp-Embed": "1"}); w.Code != http.StatusForbidden {
		t.Fatalf("missing origin: %d", w.Code)
	}

	sess := redeem(t, hn, ticket)
	token, _ := sess["token"].(string)
	if !strings.HasPrefix(token, "gse_") || sess["kind"] != models.EmbedKindSession || sess["nodeId"] != "ap1" {
		t.Fatalf("session: %+v", sess)
	}
	if w := hn.doEmbed(http.MethodPost, "/embed-api/session", map[string]any{"ticket": ticket}, redeemHeaders()); w.Code != http.StatusUnauthorized {
		t.Fatalf("replay: %d", w.Code)
	}

	// The drawer token drives the share-link chat endpoints for this node.
	prev := parseJSON(t, hn.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token}))
	if prev["status"] != models.ShareLinkStateActive || prev["kind"] != models.ShareLinkKindReview {
		t.Fatalf("preview with drawer token: %+v", prev)
	}
	if w := hn.doEmbed(http.MethodGet, "/api/runs/run-emb", nil, bearerHeader(token)); w.Code != http.StatusUnauthorized {
		t.Fatalf("api with drawer token: %d", w.Code)
	}
	dec := parseJSON(t, hn.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "nonce": "x",
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost}))
	if dec["status"] != "invalid" {
		t.Fatalf("decide with drawer token: %+v", dec)
	}

	hn.db.Model(&models.Run{}).Where("id = ?", "run-emb").Update("status", "completed")
	after := parseJSON(t, hn.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token}))
	if after["status"] == models.ShareLinkStateActive {
		t.Fatalf("finished run still active: %+v", after)
	}
}

func TestEmbedChatPageFrameAncestors(t *testing.T) {
	hn := newHarness(t)
	seedAppPreviewReview(t, hn, "run-emb-page", "ap1")
	seedDirectPreview(t, hn, "run-emb-page", "ap1")

	w := hn.doEmbed(http.MethodGet, "/embed/runs/run-emb-page/nodes/ap1/chat", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("page: %d", w.Code)
	}
	if csp := w.Header().Get("Content-Security-Policy"); csp != "frame-ancestors http://127.0.0.1:18080" {
		t.Fatalf("csp: %q", csp)
	}
	if w.Header().Get("X-Frame-Options") != "" {
		t.Fatal("drawer page must not set X-Frame-Options")
	}
	w = hn.doEmbed(http.MethodGet, "/embed/runs/run-emb-page/nodes/nope/chat", nil, nil)
	if csp := w.Header().Get("Content-Security-Policy"); csp != "frame-ancestors 'none'" {
		t.Fatalf("unknown node csp: %q", csp)
	}
}

func TestMCPEmbedOrigin(t *testing.T) {
	hn := newHarness(t)
	seedAppPreviewReview(t, hn, "run-emb-mcp", "ap1")
	seedDirectPreview(t, hn, "run-emb-mcp", "ap1")
	ticket := issueSessionTicket(t, hn, "run-emb-mcp", "ap1")
	tok := hn.host.RegisterRun("run-emb-mcp")

	path := "/mcp/runs/run-emb-mcp/embed-origin?nodeId=ap1&ticket=" + ticket
	if w := hn.doEmbed(http.MethodGet, path, nil, nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("no run token: %d", w.Code)
	}
	w := hn.doEmbed(http.MethodGet, path, nil, bearerHeader(tok))
	if w.Code != http.StatusOK {
		t.Fatalf("origin: %d %s", w.Code, w.Body.String())
	}
	if got := parseJSON(t, w)["origin"]; got != "http://example.com" {
		t.Fatalf("origin=%v", got)
	}
	if w := hn.doEmbed(http.MethodGet, "/mcp/runs/run-emb-mcp/embed-origin?nodeId=other&ticket="+ticket, nil, bearerHeader(tok)); w.Code != http.StatusNotFound {
		t.Fatalf("wrong node: %d", w.Code)
	}
	// Peeking does not consume: the drawer can still redeem.
	redeem(t, hn, ticket)
	if w := hn.doEmbed(http.MethodGet, path, nil, bearerHeader(tok)); w.Code != http.StatusNotFound {
		t.Fatalf("consumed ticket still resolves: %d", w.Code)
	}
}

func TestEmbedTicketRecordsBrowserOrigin(t *testing.T) {
	hn := newHarness(t)
	seedAppPreviewReview(t, hn, "run-emb-origin", "ap1")
	seedDirectPreview(t, hn, "run-emb-origin", "ap1")
	req := httptest.NewRequest(http.MethodPost, "/api/runs/run-emb-origin/nodes/ap1/embed-ticket", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.AddCookie(&http.Cookie{Name: "cf_session", Value: hn.cookie})
	w := httptest.NewRecorder()
	hn.r.ServeHTTP(w, req)
	ticket, _ := parseJSON(t, w)["ticket"].(string)
	c, ok := hn.h.Embed.PeekTicket(ticket)
	if !ok || c.GraspOrigin != "http://localhost:5173" {
		t.Fatalf("peek ok=%v %+v", ok, c)
	}
}

func TestPublicEmbedTicketFlow(t *testing.T) {
	hn := newHarness(t)
	seedAppPreviewReview(t, hn, "run-emb-pub", "ap1")
	seedDirectPreview(t, hn, "run-emb-pub", "ap1")
	created := parseJSON(t, hn.do(http.MethodPost, "/api/runs/run-emb-pub/reviews/ap1/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	share := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	pubHeaders := map[string]string{headerShareToken: share, headerShareRequest: "1", "Origin": "http://" + publicHost}
	res := parseJSON(t, hn.doPublic(http.MethodPost, "/public/gate-approvals/embed-ticket", nil, pubHeaders))
	ticket, _ := res["ticket"].(string)
	if res["status"] != models.ShareLinkStateActive || ticket == "" {
		t.Fatalf("public ticket: %+v", res)
	}
	sess := redeem(t, hn, ticket)
	token, _ := sess["token"].(string)
	if sess["kind"] != models.EmbedKindShare {
		t.Fatalf("kind: %+v", sess)
	}

	// Chat endpoints accept the drawer token in place of the share token.
	prev := parseJSON(t, hn.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token}))
	if prev["status"] != models.ShareLinkStateActive {
		t.Fatalf("preview with drawer token: %+v", prev)
	}
	// It can neither decide nor mint more credentials.
	dec := parseJSON(t, hn.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "nonce": "x",
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost}))
	if dec["status"] != "invalid" {
		t.Fatalf("decide with drawer token: %+v", dec)
	}
	again := parseJSON(t, hn.doPublic(http.MethodPost, "/public/gate-approvals/embed-ticket", nil, map[string]string{
		headerShareToken: token, headerShareRequest: "1", "Origin": "http://" + publicHost,
	}))
	if again["status"] != "invalid" {
		t.Fatalf("mint with drawer token: %+v", again)
	}
	if w := hn.doPublic(http.MethodPost, "/public/gate-approvals/embed-ticket", nil, map[string]string{
		headerShareToken: share, headerShareRequest: "1", "Origin": "http://evil.test",
	}); w.Code != http.StatusForbidden {
		t.Fatalf("cross-site ticket request: %d", w.Code)
	}

	// Revoking the link cuts the drawer.
	if w := hn.do(http.MethodPost, "/api/runs/run-emb-pub/reviews/ap1/share-link/revoke", nil); w.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	after := parseJSON(t, hn.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token}))
	if after["status"] == models.ShareLinkStateActive {
		t.Fatalf("drawer survived revoke: %+v", after)
	}
}

func TestPublicEventsAcceptDrawerToken(t *testing.T) {
	hn := newHarness(t)
	seedAppPreviewReview(t, hn, "run-emb-ws", "ap1")
	seedDirectPreview(t, hn, "run-emb-ws", "ap1")
	token, _ := redeem(t, hn, issueSessionTicket(t, hn, "run-emb-ws", "ap1"))["token"].(string)

	srv := httptest.NewServer(hn.r)
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/public/gate-approvals/events"

	bad, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = bad.WriteJSON(map[string]string{"token": "gse_" + strings.Repeat("0", 32)})
	var m map[string]any
	_ = bad.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := bad.ReadJSON(&m); err != nil || m["type"] != "error" {
		t.Fatalf("bad token frame: %v %+v", err, m)
	}
	bad.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.WriteJSON(map[string]string{"token": token})
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	m = nil
	if err := conn.ReadJSON(&m); err != nil || m["type"] != "ready" {
		t.Fatalf("ready: %v %+v", err, m)
	}
}
