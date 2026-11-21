package handlers_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cocofhu/grasp/internal/models"
	"github.com/cocofhu/grasp/internal/services"
)

func TestSoloSteerEndpoints(t *testing.T) {
	hn := newHarness(t)
	hn.h.Solo = services.NewSoloService(hn.db, nil, hn.h.Agents, services.NewProjectService(hn.db))
	if err := hn.db.Create(&models.SoloSession{ID: "stopped", UserID: "admin", Status: models.SoloStatusStopped}).Error; err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/solo/sessions/missing/queue/t1/steer", "/api/solo/sessions/stopped/queue/t1/steer"} {
		if res := hn.do(http.MethodPost, path, nil); res.Code != http.StatusNotFound {
			t.Fatalf("%s: %d %s", path, res.Code, res.Body)
		}
	}
	res := hn.do(http.MethodPost, "/api/solo/sessions/missing/turns", map[string]any{"text": "x", "steer": true})
	if res.Code != http.StatusNotFound {
		t.Fatalf("steered send to a missing session: %d %s", res.Code, res.Body)
	}
}

func TestNodeQueueSteerWithoutSession(t *testing.T) {
	h, _, _ := newLaneShare(t)
	for _, body := range []any{"x", map[string]any{"itemId": "nope"}} {
		if res := h.do(http.MethodPost, "/api/runs/run-lanes/nodes/design/queue/steer", body); res.Code != http.StatusBadRequest {
			t.Fatalf("node steer %v: %d %s", body, res.Code, res.Body)
		}
	}
}

func TestPublicQueueSteer(t *testing.T) {
	h, _, token := newLaneShare(t)
	steer := func(hdr map[string]string, body any) *httpResponse {
		w := h.doPublic(http.MethodPost, "/public/approvals/queue/steer", body, hdr)
		return &httpResponse{code: w.Code, body: w.Body.String()}
	}
	browser := map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost}
	visitor := map[string]string{headerShareRequest: "1", "Origin": "http://" + publicHost, headerShareVisitor: visitorID(1)}

	if r := steer(nil, map[string]any{"token": token, "itemId": "x"}); r.code != http.StatusForbidden || !strings.Contains(r.body, "csrf") {
		t.Fatalf("without csrf: %d %s", r.code, r.body)
	}
	if r := steer(browser, "x"); r.code != http.StatusBadRequest {
		t.Fatalf("bad body: %d %s", r.code, r.body)
	}
	if r := steer(browser, map[string]any{"token": "short", "itemId": "x"}); !strings.Contains(r.body, `"invalid"`) {
		t.Fatalf("malformed token: %d %s", r.code, r.body)
	}
	if r := steer(browser, map[string]any{"token": token, "itemId": "x"}); !strings.Contains(r.body, "visitor_required") {
		t.Fatalf("without a visitor: %d %s", r.code, r.body)
	}
	if r := steer(visitor, map[string]any{"token": token, "itemId": "x"}); r.code == http.StatusOK {
		t.Fatalf("unknown item on a cold lane: %d %s", r.code, r.body)
	}
}
