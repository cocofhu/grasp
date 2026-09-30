package handlers_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/handlers"
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
