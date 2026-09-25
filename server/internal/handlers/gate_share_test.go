package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cocofhu/grasp/internal/auth"
	"github.com/cocofhu/grasp/internal/gateshare"
	"github.com/cocofhu/grasp/internal/models"
)

func seedHumanGate(t *testing.T, h *harness, runID, nodeID string, actions []models.GateAction) {
	t.Helper()
	now := time.Now()
	if actions == nil {
		actions = []models.GateAction{
			{ID: "approve", Label: "批准"},
			{ID: "revise", Label: "驳回", RequireForm: true},
		}
	}
	h.db.Create(&models.WorkflowDef{
		ID: "wf-" + runID, ProjectID: models.DefaultProjectID, Name: "share-" + runID,
		Status: "published", Version: 1,
	})
	h.db.Create(&models.Run{
		ID: runID, WorkflowID: "wf-" + runID, WorkflowName: "share-" + runID, Status: "waiting_human",
		StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{{
			ID: nodeID, Type: "human_gate", Label: "审",
			Config: map[string]any{"title": "审阅", "body_template": `请审阅 {{ artifact("page.html") }}`, "actions": []any{
				map[string]any{"id": "approve", "label": "批准"},
				map[string]any{"id": "revise", "label": "驳回", "requireForm": true},
			}},
		}}},
	})
	h.db.Create(&models.Gate{
		RunID: runID, NodeID: nodeID, Iteration: 1, WorkflowID: "wf-" + runID, WorkflowName: "share-" + runID,
		Title: "审阅视觉稿", BodyMd: "请审阅 page.html，勿泄露内部 URL http://10.1.2.3/api/runs/x",
		Actions: actions, Form: []models.GateField{{Key: "comment", Label: "意见"}},
		RequestedAt: now,
	})
	h.db.Create(&models.StateRun{
		RunID: runID, NodeID: nodeID, Iteration: 1, Status: "waiting_human",
	})
	h.h.Arts.Save(runID, "visual", "page.html", "html", `<html><body><a href="/api/blobs/abc">x</a><p>ok</p></body></html>`)
	h.h.Arts.Save(runID, "visual", "clarified_requirement.json", "json", `{"title":"外部一次审批","goals":["g1"],"runId":"should-hide","projectId":"p"}`)
}

func parseJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("json: %v body=%s", err, w.Body.String())
	}
	return m
}

func TestGateShareCreateRegenRevokeAndInboxStatus(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-1", "hg1", nil)

	w := h.do(http.MethodPost, "/api/runs/run-share-1/gates/hg1/share-link", map[string]any{"ttlTier": "24h"})
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := parseJSON(t, w)
	url, _ := created["url"].(string)
	if !strings.Contains(url, "/public/gate-approvals#t=") {
		t.Fatalf("url fragment: %s", url)
	}
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")
	if !gateshare.ValidTokenShape(token) {
		t.Fatalf("token shape: %s", token)
	}
	if strings.Contains(w.Body.String(), `"token"`) && !strings.Contains(url, token) {
		t.Fatal("unexpected token field")
	}

	st := parseJSON(t, h.do(http.MethodGet, "/api/runs/run-share-1/gates/hg1/share-link", nil))
	if st["state"] != models.ShareLinkStateActive {
		t.Fatalf("status: %+v", st)
	}
	if _, ok := st["url"]; ok {
		t.Fatalf("status leaked url: %+v", st)
	}

	list := h.do(http.MethodGet, "/api/gates", nil)
	if list.Code != 200 {
		t.Fatalf("list: %d", list.Code)
	}
	if !bytes.Contains(list.Body.Bytes(), []byte(`"shareLink"`)) {
		t.Fatalf("inbox missing shareLink: %s", list.Body.String())
	}
	if !bytes.Contains(list.Body.Bytes(), []byte(`"nodeType":"human_gate"`)) {
		t.Fatalf("inbox missing nodeType=human_gate: %s", list.Body.String())
	}
	if bytes.Contains(list.Body.Bytes(), []byte(token)) {
		t.Fatal("inbox leaked plaintext token")
	}

	regen := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-1/gates/hg1/share-link/regen", nil))
	newURL, _ := regen["url"].(string)
	if newURL == url {
		t.Fatal("regen returned same url")
	}
	oldToken := token
	newToken := strings.TrimPrefix(newURL[strings.Index(newURL, "#t="):], "#t=")

	// Old token must show revoked on public preview.
	prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: oldToken})
	if parseJSON(t, prev)["status"] != models.ShareLinkStateRevoked {
		t.Fatalf("old token after regen: %s", prev.Body.String())
	}
	prev2 := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: newToken})
	p2 := parseJSON(t, prev2)
	if p2["status"] != models.ShareLinkStateActive {
		t.Fatalf("new token preview: %s", prev2.Body.String())
	}
	if strings.Contains(prev2.Body.String(), "run-share-1") || strings.Contains(prev2.Body.String(), "should-hide") || strings.Contains(prev2.Body.String(), "/api/blobs") {
		t.Fatalf("preview leak: %s", prev2.Body.String())
	}
	if p2["structured"] != nil {
		t.Fatalf("preview leaked non-primary structured as main product: %+v", p2["structured"])
	}
	if p2["visualHtml"] == nil || p2["nonce"] == "" {
		t.Fatalf("preview missing visual/nonce: %+v", p2)
	}
	if p2["productKind"] != "visual" {
		t.Fatalf("productKind=%v", p2["productKind"])
	}

	if w := h.do(http.MethodPost, "/api/runs/run-share-1/gates/hg1/share-link/revoke", nil); w.Code != 200 {
		t.Fatalf("revoke: %d %s", w.Code, w.Body.String())
	}
	prev3 := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: newToken})
	if parseJSON(t, prev3)["status"] != models.ShareLinkStateRevoked {
		t.Fatalf("after revoke: %s", prev3.Body.String())
	}

	// Recreate after revoke while still pending.
	w = h.do(http.MethodPost, "/api/runs/run-share-1/gates/hg1/share-link", map[string]any{"ttlTier": "1h"})
	if w.Code != 200 {
		t.Fatalf("recreate: %d %s", w.Code, w.Body.String())
	}
}

func TestGateShareNoStandardActionAndUsedCannotRecreate(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-2", "hg2", []models.GateAction{{ID: "custom", Label: "自定义"}})
	w := h.do(http.MethodPost, "/api/runs/run-share-2/gates/hg2/share-link", map[string]any{"ttlTier": "24h"})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "no_standard_action") {
		t.Fatalf("no standard action: %d %s", w.Code, w.Body.String())
	}

	h2 := newHarness(t)
	seedHumanGate(t, h2, "run-share-3", "hg3", nil)
	created := parseJSON(t, h2.do(http.MethodPost, "/api/runs/run-share-3/gates/hg3/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")
	nonce := publicPreviewNonce(t, h2, token)

	dec := h2.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "comment": "可以流转", "name": "Jordan", "nonce": nonce,
	}, map[string]string{
		headerShareRequest: "1",
		"Origin":           "http://" + publicHost,
	})
	if dec.Code != 200 {
		t.Fatalf("decide: %d %s", dec.Code, dec.Body.String())
	}
	body := parseJSON(t, dec)
	if body["status"] != "approved" {
		t.Fatalf("decide status: %+v", body)
	}

	w = h2.do(http.MethodPost, "/api/runs/run-share-3/gates/hg3/share-link", map[string]any{"ttlTier": "24h"})
	if w.Code != http.StatusConflict {
		t.Fatalf("used recreate: %d %s", w.Code, w.Body.String())
	}

	prev := h2.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	if parseJSON(t, prev)["status"] != models.ShareLinkStateUsed {
		t.Fatalf("used preview: %s", prev.Body.String())
	}

	// Same action retry → already processed
	nonce2 := publicPreviewNonce(t, h2, token)
	dec2 := h2.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "nonce": nonce2,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	b2 := parseJSON(t, dec2)
	if dec2.Code != 200 || b2["alreadyProcessed"] != true {
		t.Fatalf("idempotent: %d %s", dec2.Code, dec2.Body.String())
	}

	nonce3 := publicPreviewNonce(t, h2, token)
	dec3 := h2.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "revise", "comment": "nope", "nonce": nonce3,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if dec3.Code != http.StatusConflict {
		t.Fatalf("conflict: %d %s", dec3.Code, dec3.Body.String())
	}

	var evs []models.ProjectAuditEvent
	h2.db.Where("action IN ?", []string{models.AuditActionGateShareCreate, models.AuditActionGateShareUse, models.AuditActionGateDecide}).Find(&evs)
	raw, _ := json.Marshal(evs)
	if bytes.Contains(raw, []byte(token)) {
		t.Fatal("audit leaked plaintext token")
	}
	foundExternal := false
	for _, ev := range evs {
		if ev.Action == models.AuditActionGateShareUse && ev.CallerKind == models.CallerKindExternal {
			foundExternal = true
			if strings.Contains(fmtPayload(ev.Payload), token) {
				t.Fatal("use payload leaked token")
			}
		}
	}
	if !foundExternal {
		t.Fatal("missing gate.share.use callerKind=external")
	}
}

func TestGateSharePublicSecurityHeadersCSRFAndRateLimit(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-4", "hg4", nil)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-4/gates/hg4/share-link", map[string]any{"ttlTier": "8h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	page := h.doPublic(http.MethodGet, "/public/gate-approvals", nil, nil)
	if page.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("page cache: %s", page.Header().Get("Cache-Control"))
	}
	if page.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("referrer: %s", page.Header().Get("Referrer-Policy"))
	}
	if !strings.Contains(page.Header().Get("Content-Security-Policy"), "default-src 'self'") {
		t.Fatalf("csp: %s", page.Header().Get("Content-Security-Policy"))
	}
	if acao := page.Header().Get("Access-Control-Allow-Origin"); acao != "" {
		t.Fatalf("page ACAO=%q", acao)
	}

	prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	if prev.Header().Get("Cache-Control") != "no-store" || prev.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("preview headers %+v", prev.Header())
	}
	if prev.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("preview ACAO leaked")
	}

	// CSRF: missing custom header
	bad := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "nonce": "x",
	}, map[string]string{"Origin": "http://" + publicHost})
	if bad.Code != http.StatusForbidden {
		t.Fatalf("csrf missing header: %d %s", bad.Code, bad.Body.String())
	}
	// CSRF: bad origin (even with spoofed X-Forwarded-Host)
	bad2 := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "nonce": "x",
	}, map[string]string{
		headerShareRequest: "1",
		"Origin":           "https://evil.example",
		"X-Forwarded-Host": "evil.example",
	})
	if bad2.Code != http.StatusForbidden {
		t.Fatalf("csrf bad origin: %d %s", bad2.Code, bad2.Body.String())
	}

	// Third-party Origin on a non-advertise Request.Host is still rejected.
	bad3 := h.doPublicHost(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "nonce": "x",
	}, map[string]string{
		headerShareRequest: "1",
		"Origin":           "https://evil.example",
	}, "sta.internal")
	if bad3.Code != http.StatusForbidden {
		t.Fatalf("csrf third-party origin: %d %s", bad3.Code, bad3.Body.String())
	}

	// Address-bar host ≠ public_advertise: Origin matching Request.Host succeeds.
	// X-Forwarded-Host must not be trusted.
	nonceOK := publicPreviewNonce(t, h, token)
	okHost := h.doPublicHost(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "comment": "可以流转", "name": "Jordan", "nonce": nonceOK,
	}, map[string]string{
		headerShareRequest: "1",
		"Origin":           "http://sta.internal",
		"X-Forwarded-Host": "evil.example",
	}, "sta.internal")
	if okHost.Code != http.StatusOK {
		t.Fatalf("host mismatch decide: %d %s", okHost.Code, okHost.Body.String())
	}
	if st := parseJSON(t, okHost)["status"]; st != "approved" {
		t.Fatalf("host mismatch status: %v %s", st, okHost.Body.String())
	}

	// Cross-origin GET preview must not include ACAO (browser hides body).
	cross := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public/gate-approvals/preview", nil)
	req.Header.Set(headerShareToken, token)
	req.Header.Set("Origin", "https://evil.example")
	h.r.ServeHTTP(cross, req)
	if cross.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("cross ACAO=%q", cross.Header().Get("Access-Control-Allow-Origin"))
	}

	// Unknown token → invalid, no run leak
	unk := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{
		headerShareToken: strings.Repeat("ab", 32),
	})
	u := parseJSON(t, unk)
	if u["status"] != "invalid" && u["status"] != models.ShareLinkStateNone {
		t.Fatalf("unknown: %+v", u)
	}
	if strings.Contains(unk.Body.String(), "run-share") || strings.Contains(unk.Body.String(), "wf-share") {
		t.Fatalf("unknown leak: %s", unk.Body.String())
	}

	// Rate limit
	limited := false
	for i := 0; i < gateshare.PreviewRateMax+10; i++ {
		w := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
		if w.Code == http.StatusTooManyRequests {
			limited = true
			if strings.Contains(w.Body.String(), token) {
				t.Fatal("429 leaked token")
			}
			break
		}
	}
	if !limited {
		t.Fatal("expected rate limit")
	}
}

func TestGateSharePreviewPollDoesNotStarveDecide(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-rl-split", "hg-rl-split", nil)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-rl-split/gates/hg-rl-split/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")
	nonce := publicPreviewNonce(t, h, token)

	unk := strings.Repeat("ab", 32)
	limited := false
	for i := 0; i < gateshare.PreviewRateMax+10; i++ {
		w := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: unk})
		if w.Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("expected preview rate limit")
	}

	dec := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "comment": "可以流转", "name": "Jordan", "nonce": nonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if dec.Code != http.StatusOK {
		t.Fatalf("decide starved by preview poll: %d %s", dec.Code, dec.Body.String())
	}
	if parseJSON(t, dec)["status"] != "approved" {
		t.Fatalf("decide status: %s", dec.Body.String())
	}
}

func TestGateShareDecideRateLimited(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-rl-dec", "hg-rl-dec", nil)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-rl-dec/gates/hg-rl-dec/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	limited := false
	for i := 0; i < 40; i++ {
		w := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
			"token": token, "action": "approve", "nonce": "x",
		}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
		if w.Code == http.StatusTooManyRequests {
			limited = true
			body := w.Body.String()
			if !strings.Contains(body, "rate_limited") {
				t.Fatalf("429 missing rate_limited: %s", body)
			}
			if strings.Contains(body, token) {
				t.Fatal("429 leaked token")
			}
			break
		}
	}
	if !limited {
		t.Fatal("expected decide rate limit")
	}
}

func TestGateShareCSRFSecFetchSiteSameOrigin(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-sfs", "hg-sfs", nil)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-sfs/gates/hg-sfs/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	cross := h.doPublicHost(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "nonce": "x",
	}, map[string]string{
		headerShareRequest: "1",
		"Sec-Fetch-Site":   "cross-site",
	}, "sta.internal")
	if cross.Code != http.StatusForbidden {
		t.Fatalf("cross-site Sec-Fetch-Site: %d %s", cross.Code, cross.Body.String())
	}

	nonce := publicPreviewNonce(t, h, token)
	ok := h.doPublicHost(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "comment": "可以流转", "name": "Jordan", "nonce": nonce,
	}, map[string]string{
		headerShareRequest: "1",
		"Sec-Fetch-Site":   "same-origin",
	}, "sta.internal")
	if ok.Code != http.StatusOK {
		t.Fatalf("same-origin WebView decide: %d %s", ok.Code, ok.Body.String())
	}
}

func TestGateShareConcurrentDecideIdempotent(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-5", "hg5", nil)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-5/gates/hg5/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	var wg sync.WaitGroup
	results := make([]int, 8)
	wg.Add(8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			defer wg.Done()
			nonce := publicPreviewNonce(t, h, token)
			w := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
				"token": token, "action": "approve", "comment": "可以流转", "name": "Jordan", "nonce": nonce,
			}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
			results[i] = w.Code
		}(i)
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, c := range results {
		if c == 200 {
			ok++
		} else if c == http.StatusConflict || c == http.StatusForbidden {
			conflict++
		}
	}
	if ok < 1 {
		t.Fatalf("no success among %v", results)
	}
	var gate models.Gate
	h.db.Where("run_id = ? AND node_id = ?", "run-share-5", "hg5").First(&gate)
	if !gate.Resolved {
		t.Fatal("gate not resolved after concurrent decide")
	}
}

func TestGateShareUnauthorizedCannotCreate(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-6", "hg6", nil)
	w := h.doWithCookie(http.MethodPost, "/api/runs/run-share-6/gates/hg6/share-link", map[string]any{"ttlTier": "24h"}, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth create: %d %s", w.Code, w.Body.String())
	}
}

func TestGateShareIgnoresForwardedHostOnCreate(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-xfh", "hg-xfh", nil)
	b, _ := json.Marshal(map[string]any{"ttlTier": "24h"})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/run-share-xfh/gates/hg-xfh/share-link", bytes.NewReader(b))
	req.Host = "approving.example.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Host", "evil.example")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: h.cookie})
	w := httptest.NewRecorder()
	h.r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := parseJSON(t, w)
	url, _ := created["url"].(string)
	if strings.Contains(url, "evil.example") {
		t.Fatalf("share URL must ignore X-Forwarded-Host: %s", url)
	}
	if strings.Contains(url, "example.test") {
		t.Fatalf("share URL must not use PublicAdvertise: %s", url)
	}
	if !strings.Contains(url, "https://approving.example.com/public/gate-approvals#t=") {
		t.Fatalf("expected Request.Host origin, got %s", url)
	}
}

func TestGateShareMintsFromRequestHostPublicAndLoopback(t *testing.T) {
	h := newHarness(t)

	cases := []struct {
		name   string
		runID  string
		nodeID string
		host   string
		want   string
	}{
		{name: "public", runID: "run-share-host-pub", nodeID: "hg-host-pub", host: "approving.example.com", want: "http://approving.example.com/public/gate-approvals#t="},
		{name: "loopback", runID: "run-share-host-lb", nodeID: "hg-host-lb", host: "localhost:8080", want: "http://localhost:8080/public/gate-approvals#t="},
		{name: "ipv6-loopback", runID: "run-share-host-v6", nodeID: "hg-host-v6", host: "[::1]:8080", want: "http://[::1]:8080/public/gate-approvals#t="},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seedHumanGate(t, h, tc.runID, tc.nodeID, nil)
			b, _ := json.Marshal(map[string]any{"ttlTier": "24h"})
			req := httptest.NewRequest(http.MethodPost, "/api/runs/"+tc.runID+"/gates/"+tc.nodeID+"/share-link", bytes.NewReader(b))
			req.Host = tc.host
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: h.cookie})
			w := httptest.NewRecorder()
			h.r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("mint: %d %s", w.Code, w.Body.String())
			}
			url, _ := parseJSON(t, w)["url"].(string)
			if !strings.Contains(url, tc.want) {
				t.Fatalf("expected origin %q in %s", tc.want, url)
			}
			if strings.Contains(url, "example.test") {
				t.Fatalf("must not mint from PublicAdvertise: %s", url)
			}
		})
	}
}

func TestGateShareRegenFollowsCurrentHostWithoutRewritingStatus(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-host-switch", "hg-host-sw", nil)

	createBody, _ := json.Marshal(map[string]any{"ttlTier": "24h"})
	createReq := httptest.NewRequest(http.MethodPost, "/api/runs/run-share-host-switch/gates/hg-host-sw/share-link", bytes.NewReader(createBody))
	createReq.Host = "localhost:8080"
	createReq.Header.Set("Content-Type", "application/json")
	createReq.AddCookie(&http.Cookie{Name: auth.CookieName, Value: h.cookie})
	createW := httptest.NewRecorder()
	h.r.ServeHTTP(createW, createReq)
	if createW.Code != http.StatusOK {
		t.Fatalf("create: %d %s", createW.Code, createW.Body.String())
	}
	oldURL, _ := parseJSON(t, createW)["url"].(string)
	if !strings.Contains(oldURL, "http://localhost:8080/public/gate-approvals#t=") {
		t.Fatalf("loopback create: %s", oldURL)
	}

	st := parseJSON(t, h.do(http.MethodGet, "/api/runs/run-share-host-switch/gates/hg-host-sw/share-link", nil))
	if _, ok := st["url"]; ok {
		t.Fatalf("status must not rewrite/leak url: %+v", st)
	}

	regenReq := httptest.NewRequest(http.MethodPost, "/api/runs/run-share-host-switch/gates/hg-host-sw/share-link/regen", nil)
	regenReq.Host = "approving.example.com"
	regenReq.Header.Set("Content-Type", "application/json")
	regenReq.Header.Set("X-Forwarded-Proto", "https")
	regenReq.AddCookie(&http.Cookie{Name: auth.CookieName, Value: h.cookie})
	regenW := httptest.NewRecorder()
	h.r.ServeHTTP(regenW, regenReq)
	if regenW.Code != http.StatusOK {
		t.Fatalf("regen: %d %s", regenW.Code, regenW.Body.String())
	}
	newURL, _ := parseJSON(t, regenW)["url"].(string)
	if !strings.Contains(newURL, "https://approving.example.com/public/gate-approvals#t=") {
		t.Fatalf("regen must follow current Host: %s", newURL)
	}
	if newURL == oldURL {
		t.Fatal("regen returned same url")
	}
}

func TestGateShareExpiredPreviewDecideAndRecreate(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-exp", "hg-exp", nil)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-exp/gates/hg-exp/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	past := time.Now().Add(-time.Hour)
	if err := h.db.Model(&models.GateShareLink{}).Where("run_id = ?", "run-share-exp").Update("expires_at", past).Error; err != nil {
		t.Fatalf("backdate expires: %v", err)
	}

	prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	if parseJSON(t, prev)["status"] != models.ShareLinkStateExpired {
		t.Fatalf("expired preview: %s", prev.Body.String())
	}
	nonce := publicPreviewNonce(t, h, token)
	dec := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "nonce": nonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	st := parseJSON(t, dec)
	if st["status"] != models.ShareLinkStateExpired && dec.Code != http.StatusOK {
		t.Fatalf("expired decide: %d %s", dec.Code, dec.Body.String())
	}
	if st["status"] != models.ShareLinkStateExpired {
		t.Fatalf("expired decide status: %+v", st)
	}

	w := h.do(http.MethodPost, "/api/runs/run-share-exp/gates/hg-exp/share-link", map[string]any{"ttlTier": "1h"})
	if w.Code != http.StatusOK {
		t.Fatalf("recreate after expire: %d %s", w.Code, w.Body.String())
	}
	newURL, _ := parseJSON(t, w)["url"].(string)
	if newURL == url {
		t.Fatal("recreate returned same url")
	}
}

func TestGateShareLoginResumeInvalidatesUnusedLink(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-login", "hg-login", nil)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-login/gates/hg-login/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	w := h.do(http.MethodPost, "/api/runs/run-share-login/gates/hg-login/resume", map[string]any{
		"action": "approve", "form": map[string]any{},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("login resume: %d %s", w.Code, w.Body.String())
	}
	prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	st := parseJSON(t, prev)["status"]
	if st != models.ShareLinkStateRevoked && st != models.ShareLinkStateUsed {
		t.Fatalf("after login resume preview=%v body=%s", st, prev.Body.String())
	}
	re := h.do(http.MethodPost, "/api/runs/run-share-login/gates/hg-login/share-link", map[string]any{"ttlTier": "24h"})
	if re.Code != http.StatusConflict {
		t.Fatalf("recreate after login resume: %d %s", re.Code, re.Body.String())
	}
}

func TestGateShareCancelRunInvalidatesAndBlocksRecreate(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-cancel", "hg-cancel", nil)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-cancel/gates/hg-cancel/share-link", map[string]any{"ttlTier": "8h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	if w := h.do(http.MethodPost, "/api/runs/run-share-cancel/cancel", nil); w.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
	}
	prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	st := parseJSON(t, prev)["status"]
	if st != models.ShareLinkStateRevoked && st != models.ShareLinkStateUsed {
		t.Fatalf("after cancel preview=%v body=%s", st, prev.Body.String())
	}
	re := h.do(http.MethodPost, "/api/runs/run-share-cancel/gates/hg-cancel/share-link", map[string]any{"ttlTier": "24h"})
	if re.Code != http.StatusConflict || (!strings.Contains(re.Body.String(), "run_ended") && !strings.Contains(re.Body.String(), "used_readonly")) {
		t.Fatalf("recreate after cancel: %d %s", re.Code, re.Body.String())
	}
}

func TestGateSharePreviewDoesNotLeakOtherNodeArtifacts(t *testing.T) {
	h := newHarness(t)
	now := time.Now()
	runID, nodeID := "run-share-leak", "hg-leak"
	h.db.Create(&models.WorkflowDef{
		ID: "wf-" + runID, ProjectID: models.DefaultProjectID, Name: "share-" + runID,
		Status: "published", Version: 1,
	})
	h.db.Create(&models.Run{
		ID: runID, WorkflowID: "wf-" + runID, WorkflowName: "share-" + runID, Status: "waiting_human",
		StartedAt: now,
		Graph: models.Graph{Nodes: []models.Node{
			{ID: "research", Type: "research", Label: "调研", Config: map[string]any{"produces": "research.json,page.html"}},
			{ID: nodeID, Type: "human_gate", Label: "审",
				Config: map[string]any{"title": "仅说明", "body_template": "请根据说明批准，不要展示上游调研", "actions": []any{
					map[string]any{"id": "approve", "label": "批准"},
					map[string]any{"id": "revise", "label": "驳回", "requireForm": true},
				}}},
		}},
	})
	h.db.Create(&models.Gate{
		RunID: runID, NodeID: nodeID, Iteration: 1, WorkflowID: "wf-" + runID, WorkflowName: "share-" + runID,
		Title: "仅说明门禁", BodyMd: "请根据说明批准",
		Actions:     []models.GateAction{{ID: "approve", Label: "批准"}, {ID: "revise", Label: "驳回", RequireForm: true}},
		Form:        []models.GateField{{Key: "comment", Label: "意见"}},
		RequestedAt: now,
	})
	h.h.Arts.Save(runID, "research", "research.json", "json", `{"title":"LEAK-RESEARCH-TITLE","goals":["secret-goal-xyz"]}`)
	h.h.Arts.Save(runID, "research", "page.html", "html", `<html><body>LEAK-OTHER-NODE-HTML</body></html>`)

	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/"+runID+"/gates/"+nodeID+"/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")
	prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	body := prev.Body.String()
	if prev.Code != 200 {
		t.Fatalf("preview: %d %s", prev.Code, body)
	}
	if strings.Contains(body, "LEAK-RESEARCH-TITLE") || strings.Contains(body, "secret-goal-xyz") || strings.Contains(body, "LEAK-OTHER-NODE-HTML") {
		t.Fatalf("preview leaked other-node artifacts: %s", body)
	}
	p := parseJSON(t, prev)
	if p["status"] != models.ShareLinkStateActive {
		t.Fatalf("preview status: %+v", p)
	}
	if p["visualHtml"] != nil && p["visualHtml"] != "" {
		t.Fatalf("expected empty visual for description-only gate, got %+v", p["visualHtml"])
	}
	if p["structured"] != nil {
		t.Fatalf("expected no structured DTO, got %+v", p["structured"])
	}
}

func TestGateShareResumeFailureDoesNotBurnLink(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-rollback", "hg-rb", nil)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-rollback/gates/hg-rb/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	// Move the pending gate off the bound node id so resumeGateLocked fails
	// with "no pending gate" after CAS; RollbackConsume must restore the link.
	if err := h.db.Model(&models.Gate{}).Where("run_id = ? AND node_id = ?", "run-share-rollback", "hg-rb").
		Update("node_id", "hg-moved").Error; err != nil {
		t.Fatalf("move gate: %v", err)
	}

	nonce := publicPreviewNonce(t, h, token)
	dec := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "comment": "可以流转", "name": "Jordan", "nonce": nonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if dec.Code == http.StatusOK && parseJSON(t, dec)["status"] == "approved" {
		t.Fatalf("decide should fail after gate move: %s", dec.Body.String())
	}

	prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	if parseJSON(t, prev)["status"] != models.ShareLinkStateActive {
		t.Fatalf("link burned after resume failure: %s", prev.Body.String())
	}

	if err := h.db.Model(&models.Gate{}).Where("run_id = ? AND node_id = ?", "run-share-rollback", "hg-moved").
		Update("node_id", "hg-rb").Error; err != nil {
		t.Fatalf("restore gate: %v", err)
	}
	nonce2 := publicPreviewNonce(t, h, token)
	dec2 := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "comment": "可以流转", "name": "Jordan", "nonce": nonce2,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if dec2.Code != 200 || parseJSON(t, dec2)["status"] != "approved" {
		t.Fatalf("retry after restore: %d %s", dec2.Code, dec2.Body.String())
	}
}

func TestGateShareDecideRequiresNameAndComment(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-audit", "hg-audit", nil)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-audit/gates/hg-audit/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	assertActive := func() {
		t.Helper()
		prev := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
		if parseJSON(t, prev)["status"] != models.ShareLinkStateActive {
			t.Fatalf("link burned after audit reject: %s", prev.Body.String())
		}
	}

	nonce := publicPreviewNonce(t, h, token)
	noName := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "comment": "可以流转", "name": "", "nonce": nonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if noName.Code != http.StatusBadRequest || !strings.Contains(noName.Body.String(), "audit_required") {
		t.Fatalf("approve without name: %d %s", noName.Code, noName.Body.String())
	}
	assertActive()

	nonce = publicPreviewNonce(t, h, token)
	noComment := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "comment": "", "name": "Jordan", "nonce": nonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if noComment.Code != http.StatusBadRequest || !strings.Contains(noComment.Body.String(), "audit_required") {
		t.Fatalf("approve without comment: %d %s", noComment.Code, noComment.Body.String())
	}
	assertActive()

	nonce = publicPreviewNonce(t, h, token)
	rejectNoAudit := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "revise", "comment": "", "name": "", "nonce": nonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if rejectNoAudit.Code != http.StatusBadRequest || !strings.Contains(rejectNoAudit.Body.String(), "audit_required") {
		t.Fatalf("reject without audit: %d %s", rejectNoAudit.Code, rejectNoAudit.Body.String())
	}
	assertActive()

	nonce = publicPreviewNonce(t, h, token)
	ok := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "comment": "可以流转", "name": "Jordan", "nonce": nonce,
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if ok.Code != 200 || parseJSON(t, ok)["status"] != "approved" {
		t.Fatalf("approve with audit: %d %s", ok.Code, ok.Body.String())
	}
}

func TestGateShareReplyDoesNotConsume(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-reply", "hg-reply", nil)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-reply/gates/hg-reply/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	prev := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token}))
	if prev["status"] != models.ShareLinkStateActive {
		t.Fatalf("preview: %+v", prev)
	}
	if prev["kind"] != models.ShareLinkKindHumanGate && prev["kind"] != nil && prev["kind"] != "" {
		if prev["status"] != models.ShareLinkStateActive {
			t.Fatalf("kind: %+v", prev)
		}
	}
	if prev["visualHtml"] == nil {
		t.Fatalf("expected visual: %+v", prev)
	}

	reply := h.doPublic(http.MethodPost, "/public/gate-approvals/reply", map[string]any{
		"token": token, "text": "请改标题",
	}, map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost})
	if reply.Code == http.StatusForbidden {
		t.Fatalf("reply csrf: %s", reply.Body.String())
	}
	prev2 := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	if parseJSON(t, prev2)["status"] != models.ShareLinkStateActive {
		t.Fatalf("reply burned token: %d %s preview=%s", reply.Code, reply.Body.String(), prev2.Body.String())
	}
}

const (
	headerShareToken   = "X-Gate-Share-Token"
	headerShareRequest = "X-Gate-Share-Requested"
	publicHost         = "example.test"
)

func (hn *harness) doPublic(method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	return hn.doPublicHost(method, path, body, headers, publicHost)
}

func (hn *harness) doPublicHost(method, path string, body any, headers map[string]string, host string) *httptest.ResponseRecorder {
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	hn.r.ServeHTTP(w, req)
	return w
}

func publicPreviewNonce(t *testing.T, h *harness, token string) string {
	t.Helper()
	w := h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token})
	m := parseJSON(t, w)
	n, _ := m["nonce"].(string)
	if n == "" && m["status"] == models.ShareLinkStateUsed {
		return "unused"
	}
	if n == "" {
		t.Fatalf("no nonce: %s", w.Body.String())
	}
	return n
}

func fmtPayload(p map[string]any) string {
	b, _ := json.Marshal(p)
	return string(b)
}

func TestPublicGatePreviewOmitsUpstreamDocAndSupportsOnDemandUpstream(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-slim", "hg-slim", nil)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-slim/gates/hg-slim/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	prev := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token}))
	if prev["status"] != models.ShareLinkStateActive {
		t.Fatalf("preview: %+v", prev)
	}
	if prev["visualHtml"] == nil || prev["visualHtml"] == "" {
		t.Fatalf("first preview must include visualHtml: %+v", prev)
	}
	vhHash, _ := prev["visualHtmlHash"].(string)
	upHash, _ := prev["upstreamHash"].(string)
	if vhHash == "" || upHash == "" {
		t.Fatalf("expected hashes: %+v", prev)
	}
	up, _ := prev["upstream"].(map[string]any)
	if up == nil {
		t.Fatalf("expected upstream summary: %+v", prev)
	}
	if _, hasDoc := up["doc"]; hasDoc {
		t.Fatalf("open preview must not embed upstream.doc: %+v", up)
	}

	sparse := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{
		headerShareToken:                    token,
		gateshare.HeaderKnownVisualHTMLHash: vhHash,
		gateshare.HeaderKnownUpstreamHash:   upHash,
	}))
	if sparse["visualHtml"] != nil && sparse["visualHtml"] != "" {
		t.Fatalf("unchanged visualHtml must be omitted: %+v", sparse)
	}
	if sparse["upstream"] != nil {
		t.Fatalf("unchanged upstream must be omitted: %+v", sparse)
	}
	if sparse["visualHtmlHash"] != vhHash || sparse["upstreamHash"] != upHash {
		t.Fatalf("hashes must remain: %+v", sparse)
	}
	if sparse["remainingSec"] == nil {
		t.Fatalf("remainingSec still required: %+v", sparse)
	}

	full := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/upstream", nil, map[string]string{headerShareToken: token}))
	if full["status"] != models.ShareLinkStateActive {
		t.Fatalf("upstream status: %+v", full)
	}
	fullUp, _ := full["upstream"].(map[string]any)
	if fullUp == nil || fullUp["doc"] == nil {
		t.Fatalf("on-demand upstream must include doc: %+v", full)
	}
	raw, _ := json.Marshal(fullUp)
	if strings.Contains(string(raw), "should-hide") || strings.Contains(string(raw), "projectId") {
		t.Fatalf("on-demand upstream leaked: %s", raw)
	}
}

func countShareNonces(t *testing.T, h *harness) int64 {
	t.Helper()
	var n int64
	if err := h.db.Model(&models.GateShareNonce{}).Count(&n).Error; err != nil {
		t.Fatalf("count nonces: %v", err)
	}
	return n
}

func TestPublicGatePreviewSilentSkipsNonceUnlessRequested(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-nonce-idle", "hg-nonce-idle", nil)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-nonce-idle/gates/hg-nonce-idle/share-link", map[string]any{"ttlTier": "24h"}))
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	first := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token}))
	if first["nonce"] == nil || first["nonce"] == "" {
		t.Fatalf("first preview must issue nonce: %+v", first)
	}
	afterFirst := countShareNonces(t, h)
	if afterFirst < 1 {
		t.Fatalf("expected at least one nonce row, got %d", afterFirst)
	}

	silent := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{
		headerShareToken:           token,
		gateshare.HeaderSilentPoll: "1",
	}))
	if silent["nonce"] != nil && silent["nonce"] != "" {
		t.Fatalf("silent poll must omit nonce: %+v", silent)
	}
	if got := countShareNonces(t, h); got != afterFirst {
		t.Fatalf("silent poll must not Issue, rows %d → %d", afterFirst, got)
	}

	refresh := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{
		headerShareToken:           token,
		gateshare.HeaderSilentPoll: "1",
		gateshare.HeaderIssueNonce: "1",
	}))
	if refresh["nonce"] == nil || refresh["nonce"] == "" {
		t.Fatalf("silent+issueNonce must return nonce: %+v", refresh)
	}
	if got := countShareNonces(t, h); got <= afterFirst {
		t.Fatalf("issueNonce must Issue a new row, before=%d after=%d", afterFirst, got)
	}
}

func TestGateSharePermissionPresetReactOnlyDecideDenied(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-preset", "hg-preset", nil)

	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-preset/gates/hg-preset/share-link", map[string]any{
		"ttlTier": "24h", "permissionPreset": "react_only",
	}))
	if created["permissionPreset"] != models.SharePermissionReactOnly {
		t.Fatalf("create result preset: %+v", created)
	}
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	st := parseJSON(t, h.do(http.MethodGet, "/api/runs/run-share-preset/gates/hg-preset/share-link", nil))
	if st["permissionPreset"] != models.SharePermissionReactOnly {
		t.Fatalf("status preset: %+v", st)
	}

	prev := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token}))
	if prev["permissionPreset"] != models.SharePermissionReactOnly {
		t.Fatalf("preview preset: %+v", prev)
	}
	actions, _ := prev["actions"].(map[string]any)
	if _, ok := actions["confirm"]; ok {
		t.Fatalf("react_only preview still has confirm: %+v", actions)
	}
	if _, ok := actions["approve"]; ok {
		t.Fatalf("react_only preview still has approve: %+v", actions)
	}
	if _, ok := actions["reject"]; ok {
		t.Fatalf("react_only preview still has reject: %+v", actions)
	}

	nonce := publicPreviewNonce(t, h, token)
	dec := h.doPublic(http.MethodPost, "/public/gate-approvals/decide", map[string]any{
		"token": token, "action": "approve", "comment": "绕过", "name": "Eve", "nonce": nonce,
	}, map[string]string{
		headerShareRequest: "1",
		"Origin":           "http://" + publicHost,
	})
	if dec.Code != http.StatusForbidden {
		t.Fatalf("decide should 403: %d %s", dec.Code, dec.Body.String())
	}
	if !strings.Contains(dec.Body.String(), "permission_denied") {
		t.Fatalf("decide body: %s", dec.Body.String())
	}

	var link models.GateShareLink
	if err := h.db.Where("run_id = ? AND node_id = ?", "run-share-preset", "hg-preset").
		Order("created_at desc").First(&link).Error; err != nil {
		t.Fatalf("load link: %v", err)
	}
	if link.UsedAt != nil {
		t.Fatalf("react_only decide must not consume link: usedAt=%v", link.UsedAt)
	}
	var gate models.Gate
	if err := h.db.Where("run_id = ? AND node_id = ?", "run-share-preset", "hg-preset").First(&gate).Error; err != nil {
		t.Fatalf("load gate: %v", err)
	}
	if gate.Resolved {
		t.Fatal("react_only decide must not resolve gate")
	}

	regen := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-preset/gates/hg-preset/share-link/regen", nil))
	if regen["permissionPreset"] != models.SharePermissionReactOnly {
		t.Fatalf("regen must inherit preset: %+v", regen)
	}
}

func TestGateSharePermissionPresetLegacyEmptyIsFull(t *testing.T) {
	h := newHarness(t)
	seedHumanGate(t, h, "run-share-legacy", "hg-legacy", nil)
	created := parseJSON(t, h.do(http.MethodPost, "/api/runs/run-share-legacy/gates/hg-legacy/share-link", map[string]any{"ttlTier": "24h"}))
	if created["permissionPreset"] != models.SharePermissionFull {
		t.Fatalf("default create preset: %+v", created)
	}
	url, _ := created["url"].(string)
	token := strings.TrimPrefix(url[strings.Index(url, "#t="):], "#t=")

	// Simulate a legacy row with empty permission_preset.
	if err := h.db.Model(&models.GateShareLink{}).
		Where("run_id = ? AND node_id = ?", "run-share-legacy", "hg-legacy").
		Update("permission_preset", "").Error; err != nil {
		t.Fatalf("clear preset: %v", err)
	}
	prev := parseJSON(t, h.doPublic(http.MethodGet, "/public/gate-approvals/preview", nil, map[string]string{headerShareToken: token}))
	if prev["permissionPreset"] != models.SharePermissionFull {
		t.Fatalf("legacy preview preset: %+v", prev)
	}
	actions, _ := prev["actions"].(map[string]any)
	if actions["confirm"] == nil && actions["approve"] == nil {
		t.Fatalf("legacy full missing decide: %+v", actions)
	}
	st := parseJSON(t, h.do(http.MethodGet, "/api/runs/run-share-legacy/gates/hg-legacy/share-link", nil))
	if st["permissionPreset"] != models.SharePermissionFull {
		t.Fatalf("legacy status preset: %+v", st)
	}
}
